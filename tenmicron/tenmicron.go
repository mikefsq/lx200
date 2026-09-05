// Package tenmicron drives 10Micron GM-series mounts over TCP.
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

// Mount is a 10Micron mount on a LX200 connection.
type Mount struct {
	*lx200.Conn

	firmware   Version    // parsed from :GVN# at Connect; read-only after (see version.go)
	mountClass MountClass // parsed from :GVP# at Connect; read-only after (see system.go)

	mu              sync.Mutex
	cached          Status
	cachedAt        time.Time
	statusTTL       time.Duration // how long a :Ginfo# read is cached (see SetStatusTTL)
	movingPrimary   bool          // a manual MoveAxis jog is in progress on the RA/Az axis
	movingSecondary bool          // …on the Dec/Alt axis (a jog isn't in :Ginfo's slew flag)
	pulseUntil      time.Time     // a PulseGuide is active until this time (see IsPulseGuiding)
	southern        bool          // site is in the southern hemisphere (for pier inversion, see pierInverted)
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
	// Read the firmware level (best-effort): it gates the vendor-correct command forms
	// (:MSnf# goto, :PsX# park). A mount that doesn't answer :GVN# leaves the zero
	// Version, and those features fall back to their universally-supported form.
	if v, err := m.Get(":GVN#"); err == nil {
		m.firmware, _ = parseVersion(v)
	}
	// This driver derives all status from :Ginfo# (firmware ≥ 2.14.9); refuse to operate
	// on firmware too old to support it, rather than time out on every status read.
	if m.firmware != (Version{}) && !m.firmware.atLeast(2, 14, 9) {
		m.Close()
		return nil, fmt.Errorf("gotenmicron: firmware %s is too old — this driver requires ≥ 2.14.9 (for :Ginfo# status)", m.firmware)
	}
	if p, err := m.Get(":GVP#"); err == nil { // product name → mount class (altaz, GM4000)
		m.mountClass = parseMountClass(p)
	}
	if lat, err := m.SiteLatitude(); err == nil { // hemisphere for the old-firmware pier fix
		m.mu.Lock()
		m.southern = lat < 0
		m.mu.Unlock()
	}
	return m, nil
}

// SetStatusTTL sets the status cache lifetime (default 150 ms).
// A dedicated poller can call Refresh and set the TTL above its poll interval.
// It is safe to change the TTL concurrently.
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
	GstatDDSNoPower   = 12 // DDS mounts: the controller is waiting for power to be enabled
	GstatDDSMonitor   = 13 // DDS mounts: the controller is in monitor mode
	GstatDDSAutotune  = 14 // DDS mounts: the controller is in autotune mode
	GstatUnknown      = 98 // status not (yet) known
	GstatError        = 99 // mount error
)

// IsTracking reports whether the mount is actively tracking the sky. GstatOutOfLimits
// (9) counts — the spec defines it as "tracking is on but the mount is outside tracking
// limits". Unparking (3) does NOT: the spec calls it transitional motion, so it is
// reported as slewing (see IsSlewing), not tracking.
func (s Status) IsTracking() bool {
	switch s.Gstat {
	case GstatTracking, GstatOutOfLimits, GstatFollowingSat:
		return true
	}
	return false
}

// IsSlewing reports whether the mount is moving toward a target/position: an explicit
// slew (the :Ginfo# slew flag or GstatSlewing), slewing to park (GstatParking) or home
// (GstatSlewingHome), or unparking (GstatUnparking — moving from park back to operation).
// GstatDDSAutotune counts too: a direct-drive controller drives the axes while tuning
// them, so the mount is not safe to treat as settled.
func (s Status) IsSlewing() bool {
	switch s.Gstat {
	case GstatParking, GstatUnparking, GstatSlewingHome, GstatSlewing, GstatDDSAutotune:
		return true
	}
	return s.Slew
}

// IsParked reports whether the mount is parked (GstatParked).
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

// RA returns the current right ascension in hours (from :Ginfo#).
func (m *Mount) RA() (float64, error) { s, err := m.status(); return s.RA, err }

// Dec returns the current declination in degrees (from :Ginfo#).
func (m *Mount) Dec() (float64, error) { s, err := m.status(); return s.Dec, err }

// Slewing reports true while a goto/park/home is running (from :Ginfo#) OR while a
// manual MoveAxis jog is in progress — the :Ginfo# slew flag tracks gotos, not manual
// moves, so without the local flag a jog would read as "not slewing" for up to the
// status TTL.
func (m *Mount) Slewing() (bool, error) {
	if m.manuallyMoving() {
		return true, nil
	}
	s, err := m.status()
	return s.IsSlewing(), err
}

// Tracking reports whether the mount is tracking the sky (from :Ginfo#; see IsTracking).
func (m *Mount) Tracking() (bool, error) { s, err := m.status(); return s.IsTracking(), err }

// PierSide reports the side of pier from :Ginfo#, applying the southern-hemisphere
// correction on old firmware (see pierInverted).
func (m *Mount) PierSide() (lx200.PierSide, error) {
	s, err := m.status()
	p := s.Pier
	if m.pierInverted() {
		p = invertPier(p)
	}
	return p, err
}

// Altitude returns the current altitude in degrees (from :Ginfo#).
func (m *Mount) Altitude() (float64, error) { s, err := m.status(); return s.Alt, err }

// Azimuth returns the current azimuth in degrees (from :Ginfo#).
func (m *Mount) Azimuth() (float64, error) { s, err := m.status(); return s.Az, err }

// pierInverted reports whether the mount's reported side of pier must be flipped
// East↔West: firmware before 2.15.32 mis-reports the side in the southern hemisphere.
// (A no-op on any modern firmware, and when the firmware/hemisphere is unknown.) The
// vendor driver applies this to :pS#/:GTsid#; the same firmware computes the :Ginfo#
// side, so the correction is applied consistently across PierSide, PointingState and
// DestinationSideOfPier.
func (m *Mount) pierInverted() bool {
	m.mu.Lock()
	southern := m.southern
	m.mu.Unlock()
	return southern && m.firmware != (Version{}) && !m.firmware.atLeast(2, 15, 32)
}

// invertPier swaps East and West (PierUnknown is unchanged).
func invertPier(p lx200.PierSide) lx200.PierSide {
	switch p {
	case lx200.PierEast:
		return lx200.PierWest
	case lx200.PierWest:
		return lx200.PierEast
	}
	return p
}

// SlewToTarget starts a goto to the set target. It sends the vendor default :MSnf#
// (a plain slew that flips across the meridian when required) on firmware ≥ 2.11.0,
// falling back to :MS# only on older firmware that lacks :MSnf#. The fine-movement
// variant :MS# — which keeps a small move on the current pier side instead of
// flipping — is available explicitly as SlewToTargetFineLimit. Both use the LX200
// slew reply shape and invalidate the status cache.
func (m *Mount) SlewToTarget() error {
	cmd := ":MSnf#"
	if m.firmware != (Version{}) && !m.firmware.atLeast(2, 11, 0) {
		cmd = ":MS#" // firmware predates :MSnf#
	}
	return m.slewInvalidate(cmd)
}

// SlewToTargetFineLimit slews to the set target honouring the fine-movement limit
// (:MS#): a small move (target within ~2° in both axes and clear of the meridian
// limits) stays on the current side of the pier instead of flipping. This is the
// 10Micron "fine-movement slew" the plain SlewToTarget deliberately does not use.
func (m *Mount) SlewToTargetFineLimit() error { return m.slewInvalidate(":MS#") }

// SyncToTarget syncs the mount to the set target (:CM#), overriding the core only to
// invalidate the status cache. Returns the mount's sync reply.
func (m *Mount) SyncToTarget() (string, error) {
	s, err := m.Conn.SyncToTarget()
	if err == nil {
		m.invalidate()
	}
	return s, err
}

// Halt overrides the core to invalidate the cache and clear any manual-move flags, so
// Slewing() reflects the abort immediately instead of reading stale status for up to
// statusTTL.
func (m *Mount) Halt() error {
	if err := m.Conn.Halt(); err != nil {
		return err
	}
	m.clearMoving()
	m.invalidate()
	return nil
}

// MoveAxis starts a continuous manual slew on an axis and records it so Slewing()
// reports the jog (see Slewing). Otherwise inherits the core's rate-preset behaviour.
func (m *Mount) MoveAxis(a lx200.Axis, positive bool, r lx200.Rate) error {
	if err := m.Conn.MoveAxis(a, positive, r); err != nil {
		return err
	}
	m.setMoving(a, true)
	m.invalidate()
	return nil
}

// StopAxis halts manual motion on an axis and clears its moving flag.
func (m *Mount) StopAxis(a lx200.Axis) error {
	if err := m.Conn.StopAxis(a); err != nil {
		return err
	}
	m.setMoving(a, false)
	m.invalidate()
	return nil
}

// MoveAxisRate starts a continuous manual slew on an axis at an EXACT rate in degrees
// per second — the vendor's exact-rate MoveAxis, unlike the core's coarse rate-preset
// MoveAxis. It sends :RA# (primary — RA/Az) or :RE# (secondary — Dec/Alt) to set the
// rate, then the directional move; a rate ≤ 0 stops the axis. On the primary axis the
// mount's speed correction (which scales RA speed by 1/cos(dec)) is disabled for the
// move and restored afterwards, so the commanded rate is exact — matching the vendor
// driver. Slewing() reflects the move (see MoveAxis). The mount clamps the rate to its
// model's maximum slew rate (see MaxSlewRate).
func (m *Mount) MoveAxisRate(a lx200.Axis, positive bool, degPerSec float64) error {
	if degPerSec <= 0 {
		return m.StopAxis(a)
	}
	if degPerSec >= 100 { // the :RA/:RE field carries two integer digits
		return fmt.Errorf("gotenmicron: MoveAxis rate %.4f deg/s outside [0, 100)", degPerSec)
	}
	var rateCmd string
	var dir lx200.Direction
	if a == lx200.AxisPrimary {
		rateCmd = fmt.Sprintf(":RA%09.6f#", degPerSec)
		if dir = lx200.West; positive {
			dir = lx200.East
		}
		// Disable speed correction for the manual move, restoring it afterwards, so
		// the commanded RA rate isn't scaled by 1/cos(dec).
		if on, err := m.SpeedCorrection(); err == nil && on {
			if _, err := m.SetSpeedCorrection(false); err != nil {
				return err
			}
			defer func() { _, _ = m.SetSpeedCorrection(true) }()
		}
	} else {
		rateCmd = fmt.Sprintf(":RE%09.6f#", degPerSec)
		if dir = lx200.South; positive {
			dir = lx200.North
		}
	}
	if err := m.Blind(rateCmd); err != nil {
		return err
	}
	if err := m.Move(dir); err != nil {
		return err
	}
	m.setMoving(a, true)
	m.invalidate()
	return nil
}

func (m *Mount) setMoving(a lx200.Axis, v bool) {
	m.mu.Lock()
	if a == lx200.AxisPrimary {
		m.movingPrimary = v
	} else {
		m.movingSecondary = v
	}
	m.mu.Unlock()
}

func (m *Mount) clearMoving() {
	m.mu.Lock()
	m.movingPrimary, m.movingSecondary = false, false
	m.mu.Unlock()
}

func (m *Mount) manuallyMoving() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.movingPrimary || m.movingSecondary
}

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
