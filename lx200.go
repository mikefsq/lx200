// Package lx200 is a transport-agnostic core for the Meade LX200 command family
// — the `:CMD#`-framed serial/TCP protocol spoken (with vendor extensions) by
// 10Micron, Rainbow Astro (RST), ZWO (AM5/AM7) and many other mounts.
//
// A per-mount library opens the link (serial or TCP), hands the resulting
// io.ReadWriteCloser to New, and builds on the shared command set here, adding
// only its vendor-specific commands. The core fixes the framing the hand-rolled
// prototypes get wrong: every LX200 reply is one of four shapes, selected by the
// primitive you call —
//
//	Blind — no reply              (e.g. :Q#, :Mn#, :RG#)
//	Ack   — one byte '0'/'1'      (the :Sr / :Sd / :St … "set" commands)
//	Get   — read until '#'        (the :Gx# queries)
//	Slew  — :MS#: '0' = started, else a '#'-terminated fault string
//
// All commands are serialized (the mount answers only the in-flight query) and
// bounded by a read deadline.
package lx200

import (
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// DefaultTimeout bounds how long a single command waits for its reply.
const DefaultTimeout = 3 * time.Second

// Transport is the byte pipe to the mount — a serial port or a TCP connection.
// If it also implements deadliner (net.Conn does), the core sets a per-command
// read deadline; a serial transport should instead be opened with its own read
// timeout so a missing reply cannot block forever.
type Transport io.ReadWriteCloser

type deadliner interface{ SetReadDeadline(t time.Time) error }

// Conn is a synchronized LX200 command channel over a Transport.
type Conn struct {
	t       Transport
	timeout time.Duration
	mu      sync.Mutex
	opMu    sync.Mutex // serializes multi-command logical operations (see OpLock)
}

// New wraps an open transport. A zero timeout uses DefaultTimeout.
func New(t Transport, timeout time.Duration) *Conn {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Conn{t: t, timeout: timeout}
}

// DialTCP opens a TCP transport to addr ("host:port") — the usual link for
// 10Micron (3490/3492) and ZWO WiFi mounts. The returned *net.Conn satisfies
// Transport and supports per-command read deadlines.
func DialTCP(addr string, dialTimeout time.Duration) (Transport, error) {
	if dialTimeout <= 0 {
		dialTimeout = 5 * time.Second
	}
	c, err := net.DialTimeout("tcp", addr, dialTimeout)
	if err != nil {
		return nil, fmt.Errorf("lx200: dial %s: %w", addr, err)
	}
	return c, nil
}

// Close releases the transport.
func (c *Conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t.Close()
}

// OpLock serializes a multi-command logical operation — a goto or sync, which is
// the :Sr→:Sd→:MS# (or :CM#) set-target-then-act sequence — against other such
// operations on this mount, and returns the function to end it. It exists so
// independent front-ends that share one mount (the Alpaca Telescope wrapper and
// the LX200 bridge) cannot interleave their set-target sequences and leave the
// device's single target register holding one client's RA with another's Dec.
//
// It guards only the per-command mutex's blind spot: the gap *between* commands.
// Individual commands stay serialized by mu as before; OpLock is a second, outer
// lock taken for the duration of a sequence. Use it as:
//
//	defer m.OpLock()()
//	m.SetTargetRA(ra); m.SetTargetDec(dec); m.SlewToTarget()
//
// Callers that go through the Mount interface discover it via the OpLocker
// assertion; a mount that does not provide it simply runs without the outer lock.
func (c *Conn) OpLock() func() {
	c.opMu.Lock()
	return c.opMu.Unlock
}

// ErrTimeout is returned when a reply does not arrive within the command timeout.
var ErrTimeout = errors.New("lx200: timed out waiting for reply")

// Blind sends a command that produces no reply (halt, slewing-rate, move).
func (c *Conn) Blind(cmd string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.write(cmd)
}

// Ack sends an LX200 "set" command and reports whether the mount accepted it.
// These reply with a single byte: '1' = success, '0' = invalid/rejected.
func (c *Conn) Ack(cmd string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.write(cmd); err != nil {
		return false, err
	}
	b, err := c.readByte(c.timeout)
	if err != nil {
		return false, err
	}
	return b == '1', nil
}

// AckByte sends a "set" command whose reply is a single status byte (no '#')
// and returns that byte raw, for protocols whose success value is not '1' or
// varies by command — OnStep, for one, answers '0' = success for some commands
// and '1' for others. The byte is always consumed, so the next command's read
// stays in sync; the caller decides which value means success.
func (c *Conn) AckByte(cmd string) (byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.write(cmd); err != nil {
		return 0, err
	}
	return c.readByte(c.timeout)
}

// Get sends a query and returns its '#'-terminated reply (without the '#').
func (c *Conn) Get(cmd string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.write(cmd); err != nil {
		return "", err
	}
	return c.readUntil('#', c.timeout)
}

// Await reads a '#'-terminated message the mount pushes WITHOUT a prompt — e.g.
// Rainbow's asynchronous slew-completion token (:MM0#) — waiting up to timeout
// (ErrTimeout if none arrives). It is the one departure from strict request/
// response, for mounts that signal completion by an unsolicited push rather than
// a status query. Serialized with commands by the same mutex.
func (c *Conn) Await(timeout time.Duration) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if timeout <= 0 {
		timeout = c.timeout
	}
	return c.readUntil('#', timeout)
}

// GetMatching sends a query and returns the first '#'-terminated reply for which
// accept reports true, forwarding each earlier non-matching message to skip. It is
// for mounts (Rainbow RST) that interleave unsolicited completion tokens with query
// replies: an async token (:MM0#/:CHO#) can land in the buffer just ahead of the
// real reply, so the reply must be found by matching, not by position.
//
// The whole write-and-resync runs under one lock hold. Doing the same with a Get
// followed by Await — two separate lock acquisitions — leaves a gap a concurrent
// command on another goroutine can wedge into, writing its own command and stealing
// the matching reply (then everyone reads one reply off, and the missing reply
// surfaces as a spurious ErrTimeout). Holding the lock across the resync makes it
// safe when several front-ends share one mount, e.g. the Alpaca Telescope wrapper
// and the LX200 bridge polling the same RST. At most maxSkip non-matching messages
// are consumed; if none matches, the last one read is returned so the caller can
// parse it best-effort (the pre-existing behavior).
func (c *Conn) GetMatching(cmd string, accept func(string) bool, skip func(string), maxSkip int) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.write(cmd); err != nil {
		return "", err
	}
	for i := 0; ; i++ {
		s, err := c.readUntil('#', c.timeout)
		if err != nil {
			return "", err
		}
		if accept(s) || i >= maxSkip {
			return s, nil
		}
		if skip != nil {
			skip(s)
		}
	}
}

// Slew runs a slew-initiating command (classically :MS#). The mount replies '0'
// if the slew started, or a non-'0' digit followed by a '#'-terminated reason;
// Slew returns nil on success or an error carrying that reason.
func (c *Conn) Slew(cmd string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.write(cmd); err != nil {
		return err
	}
	b, err := c.readByte(c.timeout)
	if err != nil {
		return err
	}
	if b == '0' {
		return nil
	}
	reason, _ := c.readUntil('#', c.timeout) // best-effort fault text
	if reason == "" {
		return fmt.Errorf("lx200: slew rejected (code %c)", b)
	}
	return fmt.Errorf("lx200: slew rejected: %s", reason)
}

// --- framing internals (caller holds mu) ---

func (c *Conn) write(cmd string) error {
	if _, err := io.WriteString(c.t, cmd); err != nil {
		return fmt.Errorf("lx200: write %q: %w", cmd, err)
	}
	return nil
}

// setDeadline arms a per-read deadline on transports that support one (net.Conn).
// Serial transports rely on their own configured read timeout.
func (c *Conn) setDeadline(timeout time.Duration) {
	if d, ok := c.t.(deadliner); ok {
		_ = d.SetReadDeadline(time.Now().Add(timeout))
	}
}

func (c *Conn) readByte(timeout time.Duration) (byte, error) {
	c.setDeadline(timeout)
	one := make([]byte, 1)
	deadline := time.Now().Add(timeout)
	for {
		n, err := c.t.Read(one)
		if n > 0 {
			return one[0], nil
		}
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				return 0, ErrTimeout
			}
			return 0, err
		}
		if time.Now().After(deadline) { // serial timeout: Read returns (0, nil)
			return 0, ErrTimeout
		}
		time.Sleep(2 * time.Millisecond) // avoid busy-spin on a non-blocking transport
	}
}

func (c *Conn) readUntil(delim byte, timeout time.Duration) (string, error) {
	c.setDeadline(timeout)
	one := make([]byte, 1)
	deadline := time.Now().Add(timeout)
	var buf []byte
	for {
		n, err := c.t.Read(one)
		if n > 0 {
			if one[0] == delim {
				return string(buf), nil
			}
			buf = append(buf, one[0])
		}
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				return string(buf), ErrTimeout
			}
			return string(buf), err
		}
		if time.Now().After(deadline) {
			return string(buf), ErrTimeout
		}
		time.Sleep(2 * time.Millisecond) // avoid busy-spin on a non-blocking transport
	}
}
