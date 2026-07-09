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
	"math"
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
	roSite   bool                          // ACK site/clock sets without writing the mount
	logf     func(format string, a ...any) // diagnostics sink

	mu            sync.Mutex
	ln            net.Listener
	cachedProduct string // mount's real :GVP# once read (static; avoids a live round-trip per query)

	// Cached site/offset facts the bridge re-formats into Meade dialect for site and
	// date/time queries. Read from the mount lazily on the first client query that
	// needs them (they're static) and cached thereafter, so only that first identify
	// pays a round-trip and every later one answers from cache. Deliberately NOT
	// pre-warmed at startup: the bridge shares the mount's serial line with the Alpaca
	// driver, and a background poll racing the driver's just-connected, still-settling
	// link bounced the connection. The bridge now touches the mount only when a client
	// actually asks. Refreshed when a client sets them.
	siteLat, siteLon  float64       // degrees; longitude East-positive
	utcOff            time.Duration // hours added to local time to obtain UTC
	haveSite, haveOff bool
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

// WithReadOnlySite makes the bridge ACCEPT a client's site/time set commands
// (:St/:Sg/:SG/:SL/:SC reply '1') but NOT write them to the mount — for a mount with a
// pointing model, where letting an atlas overwrite the surveyed site/clock would
// invalidate the model. Reads (:Gg#/:Gt#/:GC#/…) still report the mount's real values.
func WithReadOnlySite() Option { return func(s *Server) { s.roSite = true } }

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

	// Buffered Meade time-set components (:SC date, :SL time-of-day, :SG offset). LX200
	// sets the clock in three messages; we combine them into one UTC and push it once
	// all are in (like the :Sr/:Sd/:MS target buffering).
	setDate                time.Time     // :SC, midnight of the local date
	setTOD                 time.Duration // :SL, time since local midnight
	setOff                 time.Duration // :SG, hours added to local to get UTC
	haveSC, haveSL, haveSG bool
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
	case cmd == "GVP": // product name — from the live mount when it can report one
		s.write(conn, s.productName()+"#")
	case cmd == "GVN": // firmware / version
		s.write(conn, s.version+"#")
	case cmd == "Gt": // site latitude  -> sDD*MM#
		lat, _, ok := s.site()
		if !ok {
			s.write(conn, "#")
		} else {
			s.write(conn, meadeLat(lat)+"#")
		}
	case cmd == "Gg": // site longitude -> DDD*MM# (Meade is West-positive, 0..360)
		_, lon, ok := s.site()
		if !ok {
			s.write(conn, "#")
		} else {
			s.write(conn, meadeLon(lon)+"#")
		}
	case cmd == "GG": // UTC offset -> sHH# (hours added to local to get UTC)
		s.write(conn, meadeOffset(s.offset())+"#")
	case cmd == "GL": // local time -> HH:MM:SS#
		s.write(conn, localNow(s.offset()).Format("15:04:05")+"#")
	case cmd == "GC": // calendar date -> MM/DD/YY#
		s.write(conn, localNow(s.offset()).Format("01/02/06")+"#")
	case cmd == "GW": // alignment status: <mount kind><tracking><align>#
		s.write(conn, s.alignStatus()+"#")
	case cmd == "D": // slewing? non-empty while moving, "#" when done
		s.distance(conn)
	case strings.HasPrefix(cmd, "Sr"): // set target RA
		s.setTarget(conn, cmd[2:], 0, 24, &st.tgtRA, &st.haveRA)
	case strings.HasPrefix(cmd, "Sd"): // set target Dec
		s.setTarget(conn, cmd[2:], -90, 90, &st.tgtDec, &st.haveDec)
	case strings.HasPrefix(cmd, "St"): // set site latitude (sDD*MM)
		s.setLatitude(conn, cmd[2:])
	case strings.HasPrefix(cmd, "Sg"): // set site longitude (DDD*MM, West-positive)
		s.setLongitude(conn, cmd[2:])
	case strings.HasPrefix(cmd, "SG"): // set UTC offset (sHH[.H])
		s.setUTCOffset(conn, cmd[2:], st)
	case strings.HasPrefix(cmd, "SL"): // set local time (HH:MM:SS)
		s.setLocalTime(conn, cmd[2:], st)
	case strings.HasPrefix(cmd, "SC"): // set calendar date (MM/DD/YY)
		s.setCalendarDate(conn, cmd[2:], st)
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
		// Unknown query (G…) would hang a client waiting for '#'; answer empty FAST.
		// (We deliberately do NOT forward it live to the mount: tight-timeout clients
		// like Stellarium time out on a ~100 ms round-trip and then desync, and the
		// mount's native LX200 dialect — ':' separators, ISO dates — isn't what a Meade
		// client parses anyway.) Anything non-G is a no-reply command we don't
		// implement — ignore.
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

// productName returns the connected mount's product string (:GVP#) when it can report
// one, falling back to the bridge's configured identity (WithIdent / "lx200-bridge")
// when the mount is down or doesn't implement Productizer. The real product is static,
// so the first successful read is cached — later :GVP# queries answer instantly (a
// slow live round-trip per identify is what desyncs a client with a per-command
// timeout).
func (s *Server) productName() string {
	s.mu.Lock()
	cached := s.cachedProduct
	s.mu.Unlock()
	if cached != "" {
		return cached
	}
	if m, err := s.mount(); err == nil {
		if p, ok := m.(lx200.Productizer); ok {
			if name, perr := p.Product(); perr == nil && name != "" {
				s.mu.Lock()
				s.cachedProduct = name
				s.mu.Unlock()
				return name
			}
		}
	}
	return s.product
}

// --- Meade site / date-time layer ------------------------------------------
//
// Clients like iOS Stellarium read site coordinates and date/time at connect and need
// them in classic Meade format (DDD*MM#, MM/DD/YY#, sHH#) — the mount's native LX200
// dialect (':' separators, ISO dates, signed longitude) doesn't parse. The bridge
// reads the static facts (site, UTC offset) from the mount lazily — on the first
// client query that needs them — and re-formats them; date/time come from the box
// clock shifted by the offset (the box runs at the site's zone). Only the first such
// query pays a round-trip; it is cached, so every later one answers instantly. The
// bridge intentionally does no background polling, so it never contends with the
// Alpaca driver for the mount's serial line when no client is asking.

// site returns the cached observing-site lat/lon (degrees, lon East-positive), reading
// and caching them from the mount on first use. ok is false until a SiteReader mount is
// reachable.
func (s *Server) site() (lat, lon float64, ok bool) {
	s.mu.Lock()
	if s.haveSite {
		lat, lon = s.siteLat, s.siteLon
		s.mu.Unlock()
		return lat, lon, true
	}
	s.mu.Unlock()
	m, err := s.mount()
	if err != nil {
		return 0, 0, false
	}
	r, isr := m.(lx200.SiteReader)
	if !isr {
		return 0, 0, false
	}
	la, e1 := r.SiteLatitude()
	lo, e2 := r.SiteLongitude()
	if e1 != nil || e2 != nil {
		return 0, 0, false
	}
	s.mu.Lock()
	s.siteLat, s.siteLon, s.haveSite = la, lo, true
	s.mu.Unlock()
	return la, lo, true
}

// offset returns the cached UTC offset (hours added to local to get UTC), reading it
// from the mount on first use; 0 if unavailable.
func (s *Server) offset() time.Duration {
	s.mu.Lock()
	if s.haveOff {
		d := s.utcOff
		s.mu.Unlock()
		return d
	}
	s.mu.Unlock()
	m, err := s.mount()
	if err != nil {
		return 0
	}
	r, ok := m.(lx200.UTCOffsetReader)
	if !ok {
		return 0
	}
	d, err := r.UTCOffset()
	if err != nil {
		return 0
	}
	s.mu.Lock()
	s.utcOff, s.haveOff = d, true
	s.mu.Unlock()
	return d
}

// localNow is the observing-site wall clock: box UTC minus the site's offset (offset =
// hours added to local to reach UTC, so local = UTC − offset).
func localNow(offset time.Duration) time.Time { return time.Now().UTC().Add(-offset) }

// meadeLat formats a signed latitude (degrees) as the Meade :Gt# reply sDD*MM.
func meadeLat(deg float64) string {
	sign := byte('+')
	if deg < 0 {
		sign, deg = '-', -deg
	}
	d := int(deg)
	mm := int(math.Round(deg*60)) - d*60
	if mm >= 60 {
		d++
		mm -= 60
	}
	return fmt.Sprintf("%c%02d*%02d", sign, d, mm)
}

// meadeLon formats an East-positive longitude (degrees) as the Meade :Gg# reply DDD*MM
// — West-positive, 0..360, no sign.
func meadeLon(eastDeg float64) string {
	w := math.Mod(-eastDeg, 360) // West-positive
	if w < 0 {
		w += 360
	}
	d := int(w)
	mm := int(math.Round(w*60)) - d*60
	if mm >= 60 {
		d++
		mm -= 60
	}
	if d >= 360 {
		d -= 360
	}
	return fmt.Sprintf("%03d*%02d", d, mm)
}

// meadeOffset formats the UTC offset (hours added to local to get UTC) as :GG# sHH.
func meadeOffset(d time.Duration) string {
	h := d.Hours()
	sign := byte('+')
	if h < 0 {
		sign, h = '-', -h
	}
	return fmt.Sprintf("%c%02d", sign, int(math.Round(h)))
}

// alignStatus is the Meade :GW# reply <mount kind><tracking><alignment>: the configured
// ACK mount type, T/N from the live mount, and 1 (a fleet mount with a model is aligned).
func (s *Server) alignStatus() string {
	track := byte('N')
	if m, err := s.mount(); err == nil {
		if tr, e := m.Tracking(); e == nil && tr {
			track = 'T'
		}
	}
	return fmt.Sprintf("%c%c1", s.mountTyp, track)
}

func (s *Server) setLatitude(conn net.Conn, val string) {
	deg, err := lx200.ParseSexagesimal(val)
	if err != nil || deg < -90 || deg > 90 {
		s.write(conn, "0")
		return
	}
	if s.roSite {
		s.write(conn, "1") // accepted but not applied (preserve the mount's model)
		return
	}
	m, err := s.mount()
	if err != nil {
		s.write(conn, "0")
		return
	}
	ss, ok := m.(lx200.SiteSetter)
	if !ok || ss.SetSiteLatitude(deg) != nil {
		s.write(conn, "0")
		return
	}
	s.mu.Lock()
	if s.haveSite {
		s.siteLat = deg
	}
	s.mu.Unlock()
	s.write(conn, "1")
}

func (s *Server) setLongitude(conn net.Conn, val string) {
	w, err := lx200.ParseSexagesimal(val) // Meade longitude is West-positive
	if err != nil {
		s.write(conn, "0")
		return
	}
	if s.roSite {
		s.write(conn, "1") // accepted but not applied (preserve the mount's model)
		return
	}
	east := math.Mod(-w, 360)
	if east > 180 {
		east -= 360
	} else if east < -180 {
		east += 360
	}
	m, err := s.mount()
	if err != nil {
		s.write(conn, "0")
		return
	}
	ss, ok := m.(lx200.SiteSetter)
	if !ok || ss.SetSiteLongitude(east) != nil {
		s.write(conn, "0")
		return
	}
	s.mu.Lock()
	if s.haveSite {
		s.siteLon = east
	}
	s.mu.Unlock()
	s.write(conn, "1")
}

func (s *Server) setUTCOffset(conn net.Conn, val string, st *connState) {
	h, err := lx200.ParseSexagesimal(val)
	if err != nil {
		s.write(conn, "0")
		return
	}
	if s.roSite {
		s.write(conn, "1")
		return
	}
	off := time.Duration(h * float64(time.Hour))
	st.setOff, st.haveSG = off, true
	if m, e := s.mount(); e == nil {
		if os, ok := m.(lx200.UTCOffsetSetter); ok {
			_ = os.SetUTCOffset(off)
		}
	}
	s.mu.Lock()
	s.utcOff, s.haveOff = off, true
	s.mu.Unlock()
	s.tryApplyTime(st)
	s.write(conn, "1")
}

func (s *Server) setLocalTime(conn net.Conn, val string, st *connState) {
	h, err := lx200.ParseSexagesimal(val)
	if err != nil {
		s.write(conn, "0")
		return
	}
	if s.roSite {
		s.write(conn, "1")
		return
	}
	st.setTOD, st.haveSL = time.Duration(h*float64(time.Hour)), true
	s.tryApplyTime(st)
	s.write(conn, "1")
}

func (s *Server) setCalendarDate(conn net.Conn, val string, st *connState) {
	d, err := time.Parse("01/02/06", strings.TrimSpace(val))
	if err != nil {
		s.write(conn, "0")
		return
	}
	if !s.roSite {
		st.setDate, st.haveSC = d, true
		s.tryApplyTime(st)
	}
	// Meade :SC reply: '1' then two '#'-terminated strings, which compliant clients read.
	s.write(conn, "1Updating Planetary Data#                              #")
}

// tryApplyTime pushes the buffered Meade clock set to the mount once date+time are in,
// combining them with the offset (UTC = local + offset) via the mount's SetUTC.
func (s *Server) tryApplyTime(st *connState) {
	if !st.haveSC || !st.haveSL {
		return
	}
	off := st.setOff
	if !st.haveSG {
		off = s.offset()
	}
	utc := st.setDate.Add(st.setTOD).Add(off).UTC() // local date+time, shifted to UTC
	if m, err := s.mount(); err == nil {
		if c, ok := m.(lx200.Clock); ok {
			_ = c.SetUTC(utc)
		}
	}
	st.haveSC, st.haveSL, st.haveSG = false, false, false
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
