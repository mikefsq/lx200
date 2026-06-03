// Package gotenmicron drives a 10Micron GM-series mount over the LX200 protocol
// (TCP, port 3490/3492), built on the shared golx200 core. It is the reference
// per-mount library: it embeds *lx200.Conn for the common command set and adds
// the 10Micron-specific status, tracking, park, site, and refraction/model
// commands, satisfying lx200.Mount plus the Parker/PierSider/Horizontal/
// SiteSetter/Clock optional capabilities.
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

// statusTTL coalesces the burst of getters in one Alpaca poll into a single
// :Ginfo# round-trip: the first getter fetches, the rest read the cache.
const statusTTL = 150 * time.Millisecond

// Mount is a 10Micron mount on a golx200 LX200 connection.
type Mount struct {
	*lx200.Conn

	mu       sync.Mutex
	cached   Status
	cachedAt time.Time
}

// Connect dials the mount over TCP (e.g. "10.0.1.51:3492") and switches it into
// ultra-precision mode so coordinates come back in full precision.
func Connect(addr string) (*Mount, error) {
	tr, err := lx200.DialTCP(addr, 5*time.Second)
	if err != nil {
		return nil, err
	}
	m := &Mount{Conn: lx200.New(tr, 3*time.Second)}
	if err := m.Blind(":U2#"); err != nil { // ultra precision (no reply)
		m.Close()
		return nil, fmt.Errorf("gotenmicron: set ultra-precision: %w", err)
	}
	return m, nil
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
	if !m.cachedAt.IsZero() && time.Since(m.cachedAt) < statusTTL {
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

// --- lx200.Mount (status members served from :Ginfo#) -----------------------

func (m *Mount) RA() (float64, error)  { s, err := m.status(); return s.RA, err }
func (m *Mount) Dec() (float64, error) { s, err := m.status(); return s.Dec, err }

func (m *Mount) Slewing() (bool, error)  { s, err := m.status(); return s.IsSlewing(), err }
func (m *Mount) Tracking() (bool, error) { s, err := m.status(); return s.IsTracking(), err }

// SetTracking starts (:AP#) or stops (:AL#) tracking. Both reply nothing.
func (m *Mount) SetTracking(on bool) error {
	cmd := ":AL#"
	if on {
		cmd = ":AP#"
	}
	if err := m.Blind(cmd); err != nil {
		return err
	}
	m.invalidate()
	return nil
}

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

// --- Optional capabilities --------------------------------------------------

// Parker (:KA# / :PO#, no reply; AtPark from :Ginfo Gstat).
func (m *Mount) Park() error {
	if err := m.Blind(":KA#"); err != nil {
		return err
	}
	m.invalidate()
	return nil
}

func (m *Mount) Unpark() error {
	if err := m.Blind(":PO#"); err != nil {
		return err
	}
	m.invalidate()
	return nil
}

func (m *Mount) AtPark() (bool, error) { s, err := m.status(); return s.IsParked(), err }

// PierSider / Horizontal — straight from :Ginfo#.
func (m *Mount) PierSide() (lx200.PierSide, error) { s, err := m.status(); return s.Pier, err }
func (m *Mount) Altitude() (float64, error)        { s, err := m.status(); return s.Alt, err }
func (m *Mount) Azimuth() (float64, error)         { s, err := m.status(); return s.Az, err }

// SiteSetter — 10Micron formats; longitude is East-negative (Alpaca is East-positive).
func (m *Mount) SetSiteLatitude(deg float64) error {
	return must(m.Ack(":St" + dms(deg, 2) + "#"))
}

func (m *Mount) SetSiteLongitude(deg float64) error {
	return must(m.Ack(":Sg" + dms(-deg, 3) + "#")) // negate: 10Micron East = negative
}

func (m *Mount) SetSiteElevation(meters float64) error {
	return must(m.Ack(fmt.Sprintf(":Sev%+07.1f#", meters)))
}

// Clock — set combined UTC date+time (:SUDT...#).
func (m *Mount) SetUTC(t time.Time) error {
	return must(m.Ack(t.UTC().Format(":SUDT2006-01-02,15:04:05#")))
}

// SetUTCOffset sets the offset added to local time to yield UTC (:SG). Use a
// zero offset — the mount's default working mode — so its local clock equals
// UTC and SetDate/SetTime below operate directly in UTC.
func (m *Mount) SetUTCOffset(offset time.Duration) error {
	sign := byte('+')
	if offset < 0 {
		sign, offset = '-', -offset
	}
	s := int(offset / time.Second)
	return must(m.Ack(fmt.Sprintf(":SG%c%02d:%02d:%02d#", sign, s/3600, (s/60)%60, s%60)))
}

// SetDate sets the mount's date (:SC), which the protocol expresses in local
// time; with a zero UTC offset this is the UTC date. t is read in UTC. In the
// :U2# ultra-precision mode set at Connect, :SC replies a bare "1" (no
// "Updating Planetary Data" tail), so the 1-byte ack is safe.
func (m *Mount) SetDate(t time.Time) error {
	return must(m.Ack(t.UTC().Format(":SC2006-01-02#")))
}

// SetTime sets the mount's local time (:SL); with a zero UTC offset this is the
// UTC time. t is read in UTC.
func (m *Mount) SetTime(t time.Time) error {
	return must(m.Ack(t.UTC().Format(":SL15:04:05#")))
}

// --- 10Micron vendor commands (exposed by the Alpaca wrapper as Actions) ----

// Product returns the mount product name (:GVP#).
func (m *Mount) Product() (string, error) { return m.Get(":GVP#") }

// SetRefraction sets the refraction-model pressure (hPa) and temperature (°C).
func (m *Mount) SetRefraction(pressureHPa, tempC float64) error {
	if err := must(m.Ack(fmt.Sprintf(":SRPRS%06.1f#", pressureHPa))); err != nil {
		return err
	}
	return must(m.Ack(fmt.Sprintf(":SRTMP%+06.1f#", tempC)))
}

// Refraction reads the refraction-model pressure (hPa) and temperature (°C).
func (m *Mount) Refraction() (pressureHPa, tempC float64, err error) {
	ps, err := m.Get(":GRPRS#")
	if err != nil {
		return 0, 0, err
	}
	ts, err := m.Get(":GRTMP#")
	if err != nil {
		return 0, 0, err
	}
	pressureHPa, _ = strconv.ParseFloat(strings.TrimSpace(ps), 64)
	tempC, _ = strconv.ParseFloat(strings.TrimSpace(ts), 64)
	return pressureHPa, tempC, nil
}

// SetUnattendedFlip enables/disables the unattended meridian flip (:Suaf, no reply).
func (m *Mount) SetUnattendedFlip(on bool) error {
	return m.Blind(fmt.Sprintf(":Suaf%d#", b2i(on)))
}

// SetDualAxisTracking enables/disables dual-axis tracking (:Sdat, replies 0/1).
func (m *Mount) SetDualAxisTracking(on bool) error {
	return must(m.Ack(fmt.Sprintf(":Sdat%d#", b2i(on))))
}

// Flip performs a meridian/azimuth flip (:FLIP#, replies 1 ok / 0 cannot).
func (m *Mount) Flip() error {
	if err := must(m.Ack(":FLIP#")); err != nil {
		return err
	}
	m.invalidate()
	return nil
}

// --- helpers ----------------------------------------------------------------

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
	_ lx200.Mount      = (*Mount)(nil)
	_ lx200.Parker     = (*Mount)(nil)
	_ lx200.PierSider  = (*Mount)(nil)
	_ lx200.Horizontal = (*Mount)(nil)
	_ lx200.SiteSetter = (*Mount)(nil)
	_ lx200.Clock      = (*Mount)(nil)
	_ lx200.Guider     = (*Mount)(nil)
	_ lx200.AxisMover  = (*Mount)(nil)
	_ lx200.TrackRater = (*Mount)(nil)
)
