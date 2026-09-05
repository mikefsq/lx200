// Package lx200 implements the Meade LX200 command protocol over serial and TCP.
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

// DialTCP opens a TCP transport to addr ("host:port") with read-deadline support.
// A nonpositive dialTimeout uses five seconds.
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

// OpLock locks a multi-command operation and returns its unlock function.
// All callers sharing a mount must use this lock around target-setting and
// slew/sync sequences to prevent interleaved targets. Individual commands
// remain serialized independently.
//
//	defer c.OpLock()()
func (c *Conn) OpLock() func() {
	c.opMu.Lock()
	return c.opMu.Unlock
}

// ErrTimeout is returned when a reply does not arrive within the command timeout.
var ErrTimeout = errors.New("lx200: timed out waiting for reply")

// ErrNoMatch reports that GetMatching exhausted its unmatched-reply budget.
var ErrNoMatch = errors.New("lx200: no matching reply within the skip budget")

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

// AckByte sends a command and returns its single status byte without a terminator.
// The caller interprets the command-specific success value.
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

// Await reads an unsolicited, hash-terminated message without sending a command.
// A nonpositive timeout uses the connection timeout; expiration returns ErrTimeout.
func (c *Conn) Await(timeout time.Duration) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if timeout <= 0 {
		timeout = c.timeout
	}
	return c.readUntil('#', timeout)
}

// GetMatching sends a query and reads until accept returns true, under one lock.
// It forwards up to maxSkip unmatched frames to skip, if non-nil. One further
// unmatched frame returns ErrNoMatch with no reply value.
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
		if accept(s) {
			return s, nil
		}
		if i >= maxSkip {
			return "", fmt.Errorf("%s: last frame %q: %w", cmd, s, ErrNoMatch)
		}
		if skip != nil {
			skip(s)
		}
	}
}

// SlewNack sends a command and waits for a hash-terminated refusal.
// Silence returns an empty string and nil error. A nonpositive window uses the
// connection timeout. Callers must distinguish unsolicited tokens from refusals.
func (c *Conn) SlewNack(cmd string, window time.Duration) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.write(cmd); err != nil {
		return "", err
	}
	if window <= 0 {
		window = c.timeout
	}
	s, err := c.readUntil('#', window)
	if errors.Is(err, ErrTimeout) {
		return "", nil // silence: the mount accepted it
	}
	if err != nil {
		return "", err
	}
	return s, nil
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
	// The status byte is part of the fault, not a separate thing: an RST answers a refused
	// :MS# with "MSZZ#", so reporting only what follows the first byte would log "SZZ" and
	// leave the reader hunting for a code that never appears on the wire.
	return fmt.Errorf("lx200: slew rejected: %c%s", b, reason)
}

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
