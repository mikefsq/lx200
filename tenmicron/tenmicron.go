// Package gotenmicron drives a 10Micron GM-series mount over the LX200 protocol
// (TCP, port 3490/3492), built on the shared golx200 core. It is the reference
// per-mount library: it embeds *lx200.Conn for the common command set and adds
// the 10Micron-specific status, tracking, park, site, and refraction/model
// commands, satisfying lx200.Mount plus the Parker/PierSider/Horizontal/
// SiteSetter/Clock optional capabilities.
//
// This file holds the core: the Mount type, connection, the :Ginfo# combined
// status, and the lx200.Mount status-derived members. The rest of the protocol is
// grouped by topic in sibling files (tracking.go, altaz.go, sitetime.go, dome.go,
// focuser.go, …).
package tenmicron

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mikefsq/lx200"
)

// defaultStatusTTL coalesces the burst of getters in one Alpaca poll into a single
// :Ginfo# round-trip: the first getter fetches, the rest read the cache. A consumer
// that runs its own status poller raises it past the poll interval (SetStatusTTL) so
// other front-ends ride the poller's cache rather than each refetching.
const defaultStatusTTL = 150 * time.Millisecond

// Mount is a 10Micron mount on a golx200 LX200 connection.
type Mount struct {
	*lx200.Conn

	mu        sync.Mutex
	cached    Status
	cachedAt  time.Time
	statusTTL time.Duration // how long a :Ginfo# read is cached (see SetStatusTTL)
}

// Connect dials the mount over TCP (e.g. "10.0.1.51:3492") and switches it into
// ultra-precision mode so coordinates come back in full precision.
func Connect(addr string) (*Mount, error) {
	tr, err := lx200.DialTCP(addr, 5*time.Second)
	if err != nil {
		return nil, err
	}
	m := &Mount{Conn: lx200.New(tr, 3*time.Second), statusTTL: defaultStatusTTL}
	if err := m.Blind(":U2#"); err != nil { // ultra precision (no reply)
		m.Close()
		return nil, fmt.Errorf("gotenmicron: set ultra-precision: %w", err)
	}
	return m, nil
}

// SetStatusTTL sets how long a :Ginfo# status read is cached before the next read
// refetches. The default (150 ms) just coalesces one poll's burst of getters. A
// consumer that runs its own status poller (the Alpaca driver) raises it PAST the
// poll interval and drives the cache with Refresh, so concurrent front-ends — the
// LX200 bridge, the INDI server — ride the poller's cache instead of each issuing
// their own round-trip on the single mount link. Safe to call anytime.
func (m *Mount) SetStatusTTL(d time.Duration) {
	m.mu.Lock()
	m.statusTTL = d
	m.mu.Unlock()
}

// Refresh forces a fresh :Ginfo# read (ignoring the cache TTL) and returns the decoded
// status. It is the seam a status poller calls each cycle: it keeps the cache hot so
// every other reader served from status() (RA/Dec/Slewing/… across all front-ends)
// gets the poller's value without a round-trip of its own.
func (m *Mount) Refresh() (Status, error) {
	m.invalidate()
	return m.status()
}

// --- Status (:Ginfo#) -------------------------------------------------------

// Status is the decoded :Ginfo# combined-status reply.
type Status struct {
	RA, Dec float64 // RA in hours, Dec in degrees (Jnow)
	Pier    lx200.PierSide
	Az, Alt float64 // degrees
	Gstat   int     // mount status code (:Gstat)
	Slew    bool    // slew-in-progress flag
}

// 10Micron :Gstat status codes.
const (
	GstatTracking     = 0
	GstatStopped      = 1
	GstatParking      = 2
	GstatUnparking    = 3
	GstatSlewingHome  = 4
	GstatParked       = 5
	GstatSlewing      = 6
	GstatNotTracking  = 7
	GstatMotorsCold   = 8
	GstatOutOfLimits  = 9
	GstatFollowingSat = 10
	GstatNeedsUserOK  = 11
	GstatUnknown      = 98 // status not (yet) known
	GstatError        = 99 // mount error
)

func (s Status) IsTracking() bool {
	switch s.Gstat {
	case GstatTracking, GstatUnparking, GstatOutOfLimits, GstatFollowingSat:
		return true
	}
	return false
}

func (s Status) IsSlewing() bool {
	return s.Slew || s.Gstat == GstatParking || s.Gstat == GstatSlewingHome || s.Gstat == GstatSlewing
}

func (s Status) IsParked() bool { return s.Gstat == GstatParked }

// status returns the mount status, fetching a fresh :Ginfo# only when the cache
// has expired (so a poll's many getters cost one round-trip).
func (m *Mount) status() (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ttl := m.statusTTL
	if ttl <= 0 { // a directly-constructed Mount (tests) gets the default, not "no cache"
		ttl = defaultStatusTTL
	}
	if !m.cachedAt.IsZero() && time.Since(m.cachedAt) < ttl {
		return m.cached, nil
	}
	raw, err := m.Get(":Ginfo#")
	if err != nil {
		return Status{}, err
	}
	s, err := parseGinfo(raw)
	if err != nil {
		return Status{}, err
	}
	m.cached, m.cachedAt = s, time.Now()
	return s, nil
}

// invalidate forces the next status() to refetch — called after any command that
// changes motion/tracking/park state.
func (m *Mount) invalidate() {
	m.mu.Lock()
	m.cachedAt = time.Time{}
	m.mu.Unlock()
}

// parseGinfo decodes "RA,Dec,E|W,Az,Alt,JD,Gstat,slew" (extra trailing fields,
// which 10Micron reserves the right to add, are ignored).
func parseGinfo(s string) (Status, error) {
	f := strings.Split(s, ",")
	if len(f) < 8 {
		return Status{}, fmt.Errorf("gotenmicron: short :Ginfo reply %q", s)
	}
	var st Status
	var err error
	if st.RA, err = strconv.ParseFloat(strings.TrimSpace(f[0]), 64); err != nil {
		return st, fmt.Errorf("gotenmicron: :Ginfo RA %q: %w", f[0], err)
	}
	if st.Dec, err = strconv.ParseFloat(strings.TrimSpace(f[1]), 64); err != nil {
		return st, fmt.Errorf("gotenmicron: :Ginfo Dec %q: %w", f[1], err)
	}
	switch strings.TrimSpace(f[2]) {
	case "E":
		st.Pier = lx200.PierEast
	case "W":
		st.Pier = lx200.PierWest
	default:
		st.Pier = lx200.PierUnknown
	}
	st.Az, _ = strconv.ParseFloat(strings.TrimSpace(f[3]), 64)
	st.Alt, _ = strconv.ParseFloat(strings.TrimSpace(f[4]), 64)
	// f[5] = Julian date — unused here.
	st.Gstat, _ = strconv.Atoi(strings.TrimSpace(f[6]))
	if sl, _ := strconv.Atoi(strings.TrimSpace(f[7])); sl != 0 {
		st.Slew = true
	}
	return st, nil
}

// --- lx200.Mount members served from :Ginfo# --------------------------------

func (m *Mount) RA() (float64, error)  { s, err := m.status(); return s.RA, err }
func (m *Mount) Dec() (float64, error) { s, err := m.status(); return s.Dec, err }

func (m *Mount) Slewing() (bool, error)  { s, err := m.status(); return s.IsSlewing(), err }
func (m *Mount) Tracking() (bool, error) { s, err := m.status(); return s.IsTracking(), err }

// PierSider / Horizontal — straight from :Ginfo#.
func (m *Mount) PierSide() (lx200.PierSide, error) { s, err := m.status(); return s.Pier, err }
func (m *Mount) Altitude() (float64, error)        { s, err := m.status(); return s.Alt, err }
func (m *Mount) Azimuth() (float64, error)         { s, err := m.status(); return s.Az, err }

// SlewToTarget / SyncToTarget override the core only to invalidate the cache.
func (m *Mount) SlewToTarget() error {
	if err := m.Conn.SlewToTarget(); err != nil {
		return err
	}
	m.invalidate()
	return nil
}

func (m *Mount) SyncToTarget() (string, error) {
	s, err := m.Conn.SyncToTarget()
	if err == nil {
		m.invalidate()
	}
	return s, err
}

// Halt overrides the core only to invalidate the cache, so Slewing() reflects
// the abort immediately instead of reading stale status for up to statusTTL.
func (m *Mount) Halt() error {
	if err := m.Conn.Halt(); err != nil {
		return err
	}
	m.invalidate()
	return nil
}

// --- shared helpers ---------------------------------------------------------

// must turns an LX200 set-command ack into an error.
func must(ok bool, err error) error {
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("gotenmicron: mount rejected command")
	}
	return nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// dms formats signed degrees as "sDDD*MM:SS" with a fixed degree-field width
// (2 for latitude, 3 for longitude), rounded to the nearest arcsecond.
func dms(deg float64, degWidth int) string {
	sign := byte('+')
	if deg < 0 {
		sign, deg = '-', -deg
	}
	t := int(math.Round(deg * 3600))
	return fmt.Sprintf("%c%0*d*%02d:%02d", sign, degWidth, t/3600, (t/60)%60, t%60)
}

// Compile-time proof the type satisfies the core contract + the optionals the
// Alpaca wrapper type-asserts. (Guider/AxisMover/TrackRater come from *Conn.)
var (
	_ lx200.Mount           = (*Mount)(nil)
	_ lx200.Parker          = (*Mount)(nil)
	_ lx200.Homer           = (*Mount)(nil)
	_ lx200.PierSider       = (*Mount)(nil)
	_ lx200.Horizontal      = (*Mount)(nil)
	_ lx200.SiteSetter      = (*Mount)(nil)
	_ lx200.Clock           = (*Mount)(nil)
	_ lx200.Guider          = (*Mount)(nil)
	_ lx200.AxisMover       = (*Mount)(nil)
	_ lx200.TrackRater      = (*Mount)(nil)
	_ lx200.DualAxisTracker = (*Mount)(nil)
)
