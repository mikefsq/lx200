// Package bridge serves an LX200 TCP front-end onto a lx200.Mount, letting sky
// atlases that speak the Meade LX200 telescope protocol — Stellarium's
// TelescopeControl ("Meade LX200") and SkySafari's LX200 mode — drive a mount
// that the fleet already owns.
//
// It is the server inverse of the lx200 client core: where the core sends
// :CMD# frames to a mount, the bridge answers them. It is a *consumer* of a
// lx200.Mount — exactly like the Alpaca Telescope wrapper — never a layer above
// it. The two front-ends sit side by side over the same mount, which remains the
// single source of truth:
//
//	          lx200.Mount  (the device; manages the connection)
//	         /          \
//	Alpaca Telescope     bridge.Server (this package)
//	(goalpaca-devices)    LX200 TCP for Stellarium / SkySafari
//
// State integrity across the two front-ends rests on two rules the bridge keeps:
//
//   - No cached device state. Every :GR#/:GD#/:GA#/:GZ# query reads live from the
//     Mount, so a change made through the Alpaca side is visible here at once and
//     vice-versa. The mount's own per-command serialization makes each read
//     consistent.
//   - Atomic target writes. The LX200 client sends :Sr (RA), :Sd (Dec) and :MS#
//     (slew) as three separate messages; the bridge buffers RA/Dec per connection
//     (the client's intent — the LX200 analogue of Alpaca's remembered
//     TargetRightAscension) and writes the device's target register only inside an
//     OpLock-guarded SetTarget→act sequence at :MS#/:CM# time. With the Alpaca
//     wrapper taking the same OpLock for its slews, the two front-ends can never
//     interleave their set-target sequences and leave the mount aiming at one
//     client's RA with another's Dec.
//
// NexStar (SkySafari's other mode) is the same semantics over different framing;
// it can be added as a second front-end over the same Mount source without
// touching this file.
package bridge

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/mikefsq/lx200"
)

// idleTimeout bounds how long a connection may sit silent before the bridge drops
// it. Atlases poll position every second or two, so a quiet minute means the peer
// is gone; dropping it reclaims the goroutine.
const idleTimeout = 2 * time.Minute

// MountFunc returns the mount that is live right now, or an error if it is not
// currently connected. The bridge calls it once per operation rather than holding
// a handle, so a reconnect on the owning side is picked up transparently and the
// bridge never caches a stale mount or any device state.
type MountFunc func() (lx200.Mount, error)

// Server is an LX200 TCP server over a MountFunc. The zero value is not usable;
// build one with New.
type Server struct {
	addr     string
	mount    MountFunc
	mountTyp byte                          // ACK (0x06) reply: alignment/mount kind
	product  string                        // :GVP# product-name reply
	version  string                        // :GVN# firmware/version reply
	logf     func(format string, a ...any) // diagnostics sink

	mu sync.Mutex
	ln net.Listener
}

// Option configures a Server.
type Option func(*Server)

// WithMountType sets the byte returned for the LX200 ACK (0x06) alignment query:
// 'P' polar/equatorial (the default), 'A' alt-az, 'G' German-equatorial, 'L'
// land. Atlases use it only to confirm a mount is answering.
func WithMountType(b byte) Option { return func(s *Server) { s.mountTyp = b } }

// WithIdent sets the product name (:GVP#) and firmware/version (:GVN#) strings.
func WithIdent(product, version string) Option {
	return func(s *Server) { s.product, s.version = product, version }
}

// WithLogger sets a diagnostics sink (e.g. log.Printf). Nil disables logging.
func WithLogger(logf func(format string, a ...any)) Option {
	return func(s *Server) { s.logf = logf }
}

// New builds a Server that will listen on addr ("host:port" or ":port") and serve
// the mount returned by m.
func New(addr string, m MountFunc, opts ...Option) *Server {
	s := &Server{
		addr:     addr,
		mount:    m,
		mountTyp: 'P',
		product:  "lx200-bridge",
		version:  "1.0",
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Serve listens and accepts connections until ctx is cancelled, one goroutine per
// connection. It returns nil on a clean ctx-driven shutdown, or the listen/accept
// error otherwise.
func (s *Server) Serve(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("bridge: listen %s: %w", s.addr, err)
	}
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()
	go func() { <-ctx.Done(); _ = ln.Close() }()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil // shutdown
			}
			return fmt.Errorf("bridge: accept: %w", err)
		}
		go s.handle(ctx, conn)
	}
}

// Addr is the address the server is listening on (valid after Serve has started).
func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln == nil {
		return nil
	}
	return s.ln.Addr()
}

func (s *Server) log(format string, a ...any) {
	if s.logf != nil {
		s.logf(format, a...)
	}
}

// connState is the per-connection buffered target — the client's intent, not
// device truth. It is the LX200 analogue of the Alpaca driver-remembered
// TargetRightAscension/TargetDeclination, and is written to the device only at
// :MS#/:CM# time inside an OpLock.
type connState struct {
	tgtRA, tgtDec   float64
	haveRA, haveDec bool
}

func (s *Server) handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	go func() { <-ctx.Done(); _ = conn.Close() }() // unblock a parked Read on shutdown

	br := bufio.NewReader(conn)
	var st connState
	for {
		_ = conn.SetReadDeadline(time.Now().Add(idleTimeout))
		b, err := br.ReadByte()
		if err != nil {
			return // peer closed, idle timeout, or shutdown
		}
		switch b {
		case 0x06: // ACK — alignment/mount-kind query, single-char reply, no '#'
			s.write(conn, string(s.mountTyp))
		case ':': // a :CMD# frame
			body, err := br.ReadString('#')
			if err != nil {
				return
			}
			s.dispatch(conn, strings.TrimSuffix(body, "#"), &st)
		default:
			// Stray byte between frames (e.g. CR/LF some clients append). Ignore
			// and resync on the next ':' or ACK.
		}
	}
}

func (s *Server) write(conn net.Conn, msg string) {
	if _, err := conn.Write([]byte(msg)); err != nil {
		s.log("bridge: write %q: %v", msg, err)
	}
}

// dispatch handles one LX200 command body (no leading ':' or trailing '#').
func (s *Server) dispatch(conn net.Conn, cmd string, st *connState) {
	switch {
	case cmd == "GR": // current RA, high precision
		s.getCoord(conn, func(m lx200.Mount) (float64, error) { return m.RA() }, formatRA)
	case cmd == "GD": // current Dec
		s.getCoord(conn, func(m lx200.Mount) (float64, error) { return m.Dec() }, formatDec)
	case cmd == "GA": // current Altitude
		s.getCoord(conn, altitude, formatDec)
	case cmd == "GZ": // current Azimuth
		s.getCoord(conn, azimuth, formatDec)
	case cmd == "GVP": // product name
		s.write(conn, s.product+"#")
	case cmd == "GVN": // firmware / version
		s.write(conn, s.version+"#")
	case cmd == "D": // slewing? non-empty while moving, "#" when done
		s.distance(conn)
	case strings.HasPrefix(cmd, "Sr"): // set target RA
		s.setTarget(conn, cmd[2:], 0, 24, &st.tgtRA, &st.haveRA)
	case strings.HasPrefix(cmd, "Sd"): // set target Dec
		s.setTarget(conn, cmd[2:], -90, 90, &st.tgtDec, &st.haveDec)
	case cmd == "MS": // slew to buffered target
		s.gotoTarget(conn, st)
	case cmd == "CM": // sync (align) to buffered target
		s.syncTarget(conn, st)
	case cmd == "Q": // halt all motion
		s.halt(conn)
	case cmd == "U": // toggle precision — we always emit high precision; no reply
	case len(cmd) == 2 && cmd[0] == 'M' && isDir(cmd[1]): // :Mn/:Ms/:Me/:Mw#
		s.move(cmd[1])
	case len(cmd) == 2 && cmd[0] == 'Q' && isDir(cmd[1]): // :Qn/:Qs/:Qe/:Qw#
		s.haltMove(cmd[1])
	case len(cmd) == 2 && cmd[0] == 'R' && strings.IndexByte("GCMS", cmd[1]) >= 0: // :RG/:RC/:RM/:RS#
		s.setRate(cmd[1])
	default:
		// Unknown query (G…) would hang a client waiting for '#'; answer empty.
		// Anything else is a no-reply command we don't implement — ignore.
		if strings.HasPrefix(cmd, "G") {
			s.write(conn, "#")
		}
	}
}

// --- coordinate reads (always live, never cached) ---

func (s *Server) getCoord(conn net.Conn, read func(lx200.Mount) (float64, error), format func(float64) string) {
	m, err := s.mount()
	if err != nil {
		s.write(conn, "#") // not connected: empty reply keeps the client unblocked
		return
	}
	v, err := read(m)
	if err != nil {
		s.log("bridge: read coord: %v", err)
		s.write(conn, "#")
		return
	}
	s.write(conn, format(v)+"#")
}

func altitude(m lx200.Mount) (float64, error) {
	if h, ok := m.(lx200.Horizontal); ok {
		return h.Altitude()
	}
	return 0, nil
}

func azimuth(m lx200.Mount) (float64, error) {
	if h, ok := m.(lx200.Horizontal); ok {
		return h.Azimuth()
	}
	return 0, nil
}

func (s *Server) distance(conn net.Conn) {
	m, err := s.mount()
	if err != nil {
		s.write(conn, "#")
		return
	}
	slewing, err := m.Slewing()
	if err == nil && slewing {
		s.write(conn, "|#") // non-empty payload = still slewing (LX200 convention)
		return
	}
	s.write(conn, "#") // empty = slew complete / not moving
}

// --- target buffering + atomic goto/sync ---

func (s *Server) setTarget(conn net.Conn, val string, lo, hi float64, dst *float64, have *bool) {
	v, err := lx200.ParseSexagesimal(val)
	if err != nil || v < lo || v >= hi+1e-9 {
		*have = false
		s.write(conn, "0") // rejected
		return
	}
	*dst, *have = v, true
	s.write(conn, "1") // accepted (buffered; not yet written to the device)
}

func (s *Server) gotoTarget(conn net.Conn, st *connState) {
	if !st.haveRA || !st.haveDec {
		s.write(conn, "1No target set#")
		return
	}
	m, err := s.mount()
	if err != nil {
		s.write(conn, "1"+err.Error()+"#")
		return
	}
	err = withOp(m, func() error {
		if err := setDeviceTarget(m, st.tgtRA, st.tgtDec); err != nil {
			return err
		}
		return m.SlewToTarget()
	})
	if err != nil {
		s.write(conn, "1"+oneLine(err.Error())+"#")
		return
	}
	s.write(conn, "0") // slew started
}

func (s *Server) syncTarget(conn net.Conn, st *connState) {
	if !st.haveRA || !st.haveDec {
		s.write(conn, "No target set#")
		return
	}
	m, err := s.mount()
	if err != nil {
		s.write(conn, "Not connected#")
		return
	}
	var matched string
	err = withOp(m, func() error {
		if err := setDeviceTarget(m, st.tgtRA, st.tgtDec); err != nil {
			return err
		}
		var e error
		matched, e = m.SyncToTarget()
		return e
	})
	if err != nil {
		s.log("bridge: sync: %v", err)
		s.write(conn, "Sync failed#")
		return
	}
	if matched == "" {
		matched = "Coordinates matched."
	}
	s.write(conn, matched+"#")
}

// setDeviceTarget writes the device's target register. The caller holds the
// OpLock so this Set-RA-then-Set-Dec pair cannot interleave with another
// front-end's.
func setDeviceTarget(m lx200.Mount, ra, dec float64) error {
	if ok, err := m.SetTargetRA(ra); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("mount rejected target RA")
	}
	if ok, err := m.SetTargetDec(dec); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("mount rejected target Dec")
	}
	return nil
}

// withOp runs f under the mount's OpLock if it provides one, serializing the
// whole set-target-then-act sequence against the Alpaca wrapper's slews. A mount
// without OpLock (none in this fleet) simply runs f directly.
func withOp(m lx200.Mount, f func() error) error {
	if l, ok := m.(lx200.OpLocker); ok {
		defer l.OpLock()()
	}
	return f()
}

// --- halt / manual motion (manual motion is optional per mount) ---

func (s *Server) halt(conn net.Conn) {
	if m, err := s.mount(); err == nil {
		if err := m.Halt(); err != nil {
			s.log("bridge: halt: %v", err)
		}
	}
	// :Q# has no reply.
}

// mover and rater are the optional manual-motion capabilities; *lx200.Conn
// satisfies both, so any per-mount type that embeds it does too.
type mover interface {
	Move(lx200.Direction) error
	HaltMove(lx200.Direction) error
}
type rater interface {
	SetRate(lx200.Rate) error
}

func (s *Server) move(dir byte) {
	m, err := s.mount()
	if err != nil {
		return
	}
	if mv, ok := m.(mover); ok {
		if err := mv.Move(lx200.Direction(dir)); err != nil {
			s.log("bridge: move %c: %v", dir, err)
		}
	}
}

func (s *Server) haltMove(dir byte) {
	m, err := s.mount()
	if err != nil {
		return
	}
	if mv, ok := m.(mover); ok {
		if err := mv.HaltMove(lx200.Direction(dir)); err != nil {
			s.log("bridge: halt-move %c: %v", dir, err)
		}
	}
}

func (s *Server) setRate(r byte) {
	m, err := s.mount()
	if err != nil {
		return
	}
	if rt, ok := m.(rater); ok {
		if err := rt.SetRate(lx200.Rate(r)); err != nil {
			s.log("bridge: set-rate %c: %v", r, err)
		}
	}
}

// --- formatting helpers ---

func formatRA(hours float64) string { return lx200.FormatHMS(hours) }
func formatDec(deg float64) string  { return lx200.FormatDMS(deg, '*') }
func isDir(b byte) bool             { return b == 'n' || b == 's' || b == 'e' || b == 'w' }
func oneLine(s string) string       { return strings.ReplaceAll(strings.ReplaceAll(s, "#", " "), "\n", " ") }
