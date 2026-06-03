// Package rainbow drives Rainbow Astro RST harmonic mounts (RST-135/300) over
// their LX200-derived serial dialect, on the shared golx200 core. RST is the
// outlier of the LX200 family: it has no status query — instead it pushes an
// unsolicited completion token (:MM0# / :CHO#) when a slew or home finishes — and
// its replies echo the command prefix (:GR# -> :GR20:28:56.9#). Both are handled
// here. Protocol details are reverse-engineered from app captures (see hubo/).
package rst

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mikefsq/lx200"
	"github.com/mikefsq/lx200/serial"
)

// Rainbow USB-serial: FTDI, 115200 baud.
const baud = 115200

// Token-peek tuning: how long Slewing()/coord reads wait for a pushed completion
// token, and how often (coalescing a poll burst into one peek).
const (
	peekTimeout = 120 * time.Millisecond
	peekTTL     = 100 * time.Millisecond
)

// Mount is a Rainbow Astro RST mount on a golx200 LX200 connection.
type Mount struct {
	*lx200.Conn

	mu        sync.Mutex
	slewing   bool      // set on goto/home/park; cleared by the completion token
	lastPeek  time.Time // coalesces token peeks across a poll burst
	lastFault string
	parked    bool
	targetRA  float64 // remembered for the degrees-based :Ck sync
	targetDec float64
	pulsing   bool
}

// Open connects over USB-serial at 115200 baud.
func Open(portName string) (*Mount, error) {
	tr, err := serial.Open(portName, baud)
	if err != nil {
		return nil, err
	}
	return &Mount{Conn: lx200.New(tr, 3*time.Second)}, nil
}

// Find auto-detects the RST by its FTDI USB id (VID 0403 / PID 6001) and opens
// the first match.
func Find() (*Mount, error) {
	ports, err := serial.List()
	if err != nil {
		return nil, err
	}
	for _, p := range ports {
		if p.IsUSB && p.VID == "0403" && p.PID == "6001" {
			return Open(p.Name)
		}
	}
	return nil, fmt.Errorf("rainbow: no RST mount found (FTDI 0403:6001)")
}

// Version returns the firmware version (:AV#).
func (m *Mount) Version() (string, error) { return m.get(":AV#", ":AV") }

// --- Unsolicited completion-token handling (the RST-specific core) -----------

// drainToken peeks for a pushed slew/home completion token, but only while a
// goto/home/park is in flight and at most once per peekTTL (so a poll's burst of
// getters costs one peek). Draining BEFORE any coord read keeps the token from
// being mistaken for a coordinate reply.
func (m *Mount) drainToken() {
	m.mu.Lock()
	if !m.slewing || time.Since(m.lastPeek) < peekTTL {
		m.mu.Unlock()
		return
	}
	m.lastPeek = time.Now()
	m.mu.Unlock()

	tok, err := m.Await(peekTimeout)
	if err != nil { // ErrTimeout: nothing pushed yet, still slewing
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	switch tok {
	case ":MM0", ":CHO": // slew / home complete
		m.slewing = false
	case ":MML", ":MMU", ":MME", ":CH0", ":CH<": // faults
		m.slewing = false
		m.lastFault = tok
	}
}

func (m *Mount) setSlewing(v bool) {
	m.mu.Lock()
	m.slewing = v
	m.lastPeek = time.Time{}
	m.mu.Unlock()
}

// --- Coordinate reads: strip the echoed prefix so the sign leads ------------

// get sends a query and strips the echoed command prefix from the reply
// (:GR20:28:56.9# -> "20:28:56.9").
func (m *Mount) get(cmd, prefix string) (string, error) {
	s, err := m.Get(cmd)
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(s, prefix), nil
}

func (m *Mount) coord(cmd, prefix string) (float64, error) {
	m.drainToken()
	s, err := m.get(cmd, prefix)
	if err != nil {
		return 0, err
	}
	return lx200.ParseSexagesimal(s)
}

func (m *Mount) RA() (float64, error)       { return m.coord(":GR#", ":GR") }
func (m *Mount) Dec() (float64, error)      { return m.coord(":GD#", ":GD") }
func (m *Mount) Altitude() (float64, error) { return m.coord(":GA#", ":GA") }
func (m *Mount) Azimuth() (float64, error)  { return m.coord(":GZ#", ":GZ") }

// --- Target + goto/sync (RST dialect) ---------------------------------------

// SetTargetRA/Dec remember the target (for the degrees-based sync) and send the
// RST tenth-of-a-second format. (Ack framing verified on hardware.)
func (m *Mount) SetTargetRA(hours float64) (bool, error) {
	m.mu.Lock()
	m.targetRA = hours
	m.mu.Unlock()
	h, mm, s := hms(hours)
	return m.Ack(fmt.Sprintf(":Sr%02d:%02d:%04.1f#", h, mm, s))
}

func (m *Mount) SetTargetDec(deg float64) (bool, error) {
	m.mu.Lock()
	m.targetDec = deg
	m.mu.Unlock()
	sign, d, mm, s := dmsParts(deg)
	return m.Ack(fmt.Sprintf(":Sd%c%02d*%02d:%04.1f#", sign, d, mm, s))
}

// SlewToTarget starts an equatorial goto (:MS#). RST replies nothing; completion
// arrives later as the :MM0# token, so we mark slewing and let drainToken clear it.
func (m *Mount) SlewToTarget() error {
	if err := m.Blind(":MS#"); err != nil {
		return err
	}
	m.setSlewing(true)
	return nil
}

// syncCmd builds RST's degrees-based sync command from the remembered target.
// verb is ":Ck" (re-center only) or ":CN" (also save a star-alignment point).
func (m *Mount) syncCmd(verb string) string {
	m.mu.Lock()
	ra, dec := m.targetRA, m.targetDec
	m.mu.Unlock()
	sign := byte('+')
	if dec < 0 {
		sign, dec = '-', -dec
	}
	return fmt.Sprintf("%s%07.3f%c%06.3f#", verb, ra*15, sign, dec)
}

// SyncToTarget re-centers on the remembered target (:Ck) without changing the
// alignment model.
func (m *Mount) SyncToTarget() (string, error) {
	return "", m.Blind(m.syncCmd(":Ck"))
}

// AddAlignmentPoint syncs to the remembered target and saves it as a star-
// alignment point (:CN) — INDI's "save alignment before sync" behavior, for
// building a multi-point model. Set the target with SetTargetRA/Dec first.
func (m *Mount) AddAlignmentPoint() error {
	return m.Blind(m.syncCmd(":CN"))
}

// Halt stops motion (:Q#) and ends the slew state.
func (m *Mount) Halt() error {
	if err := m.Blind(":Q#"); err != nil {
		return err
	}
	m.setSlewing(false)
	return nil
}

// --- Status (no status command: tracking via :AT#, slewing via the token) ----

func (m *Mount) Slewing() (bool, error) {
	m.drainToken()
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.slewing, nil
}

// Tracking reads :AT# (-> :AT0# / :AT1#).
func (m *Mount) Tracking() (bool, error) {
	s, err := m.get(":AT#", ":AT")
	if err != nil {
		return false, err
	}
	return strings.HasPrefix(s, "1"), nil
}

// SetTracking enables (:CtA#) / disables (:CtL#) tracking.
func (m *Mount) SetTracking(on bool) error {
	cmd := ":CtL#"
	if on {
		cmd = ":CtA#"
	}
	return m.Blind(cmd)
}

// TrackRater — RST track modes are :CtR/:CtS/:CtL (not the core's :TQ/:TS/:TL).
// Note :CtL# also doubles as tracking-disable (matches INDI).
func (m *Mount) TrackSidereal() error { return m.Blind(":CtR#") }
func (m *Mount) TrackSolar() error    { return m.Blind(":CtS#") }
func (m *Mount) TrackLunar() error    { return m.Blind(":CtL#") }

// TrackMode is the active tracking rate reported by :Ct?#.
type TrackMode int

const (
	TrackModeSidereal TrackMode = iota // :CtR# (:Ct?# -> :CT0#)
	TrackModeSolar                     // :CtS# (:Ct?# -> :CT1#)
	TrackModeCustom                    // :CtM#/:CtU# (:Ct?# -> :CT2#)
)

// TrackMode reads the active tracking rate via :Ct?# (-> :CT0#/:CT1#/:CT2#).
// The mapping is capture-derived; :Ct?# is an RST extra (INDI reports only
// tracking on/off, via :AT#).
func (m *Mount) TrackMode() (TrackMode, error) {
	s, err := m.get(":Ct?#", ":CT")
	if err != nil {
		return 0, err
	}
	switch {
	case strings.HasPrefix(s, "2"):
		return TrackModeCustom, nil
	case strings.HasPrefix(s, "1"):
		return TrackModeSolar, nil
	default:
		return TrackModeSidereal, nil
	}
}

// --- Homer (:Ch#, completion via :CHO#; at-home from :GH#) -------------------

func (m *Mount) FindHome() error {
	if err := m.Blind(":Ch#"); err != nil {
		return err
	}
	m.setSlewing(true)
	return nil
}

func (m *Mount) AtHome() (bool, error) {
	s, err := m.get(":GH#", ":GH")
	if err != nil {
		return false, err
	}
	return strings.HasPrefix(s, "O"), nil // "O" homed, "0" not
}

// --- Parker (software park: slew to a stored Az/Alt; no native park) ---------

func (m *Mount) Park() error {
	// Stow at Az 180, Alt 0 (set Az/Alt target, then horizontal goto).
	if _, err := m.Ack(":Sz180*00'00.0\"#"); err != nil {
		return err
	}
	if _, err := m.Ack(":Sa+00*00'00.0\"#"); err != nil {
		return err
	}
	if err := m.Blind(":MA#"); err != nil {
		return err
	}
	m.mu.Lock()
	m.parked = true
	m.mu.Unlock()
	m.setSlewing(true)
	return nil
}

func (m *Mount) Unpark() error {
	m.mu.Lock()
	m.parked = false
	m.mu.Unlock()
	return m.SetTracking(true)
}

func (m *Mount) AtPark() (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.parked, nil
}

// --- PierSider (RST has no pier flag; derive it like INDI) -------------------

// PierSide derives the side of pier from the Dec-axis mechanical angle (:CY#,
// "+232.22/-090.06") minus the Dec-axis alignment offset (:CG3#, "+000.0000"):
// an effective angle past 90 degrees means the tube is on the west side.
func (m *Mount) PierSide() (lx200.PierSide, error) {
	m.drainToken()
	offRaw, err := m.get(":CG3#", ":CG3")
	if err != nil {
		return lx200.PierUnknown, err
	}
	cyRaw, err := m.get(":CY#", ":CY")
	if err != nil {
		return lx200.PierUnknown, err
	}
	if i := strings.IndexByte(cyRaw, '/'); i >= 0 {
		cyRaw = cyRaw[:i] // keep the Dec-axis field before the '/'
	}
	var alignOff, decAxis float64
	if _, err := fmt.Sscanf(offRaw, "%f", &alignOff); err != nil {
		return lx200.PierUnknown, nil
	}
	if _, err := fmt.Sscanf(cyRaw, "%f", &decAxis); err != nil {
		return lx200.PierUnknown, nil
	}
	if decAxis-alignOff > 90 {
		return lx200.PierWest, nil
	}
	return lx200.PierEast, nil
}

// --- Guider — RST has no :Mg pulse-guide; it times a start/stop move ---------

// PulseGuide guides for ms milliseconds: RST forces custom track + guide rate,
// starts a move, and (asynchronously) stops it. Returns immediately.
func (m *Mount) PulseGuide(d lx200.Direction, ms int) error {
	if err := m.Blind(":CtU#"); err != nil { // custom (guide) tracking
		return err
	}
	if err := m.SetRate(lx200.RateGuide); err != nil { // :RG#
		return err
	}
	if err := m.Move(d); err != nil {
		return err
	}
	m.mu.Lock()
	m.pulsing = true
	m.mu.Unlock()
	go func() {
		time.Sleep(time.Duration(ms) * time.Millisecond)
		_ = m.Blind(":Q#") // RST has no directional quit — bare :Q# stops the move
		m.mu.Lock()
		m.pulsing = false
		m.mu.Unlock()
	}()
	return nil
}

func (m *Mount) IsPulseGuiding() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pulsing
}

// StopAxis overrides the core AxisMover: RST has no directional quit
// (:Qn/:Qs/:Qe/:Qw), only the bare :Q# full-stop. MoveAxis stays inherited
// (RST uses the core's letter rates, confirmed by the captures).
func (m *Mount) StopAxis(a lx200.Axis) error { return m.Blind(":Q#") }

// --- SiteSetter (RST uses '*' separators; longitude is East-negative) --------

func (m *Mount) SetSiteLatitude(deg float64) error {
	sign, d, mm, s := dmsParts(deg)
	return ack(m.Ack(fmt.Sprintf(":St%c%02d*%02d*%02d#", sign, d, mm, int(s))))
}

func (m *Mount) SetSiteLongitude(deg float64) error {
	sign, d, mm, s := dmsParts(-deg) // negate: RST East = negative
	return ack(m.Ack(fmt.Sprintf(":Sg%c%03d*%02d*%02d#", sign, d, mm, int(s))))
}

// SetSiteElevation is a no-op: the RST protocol has no site-elevation command
// (elevation does not affect its pointing). Present to satisfy SiteSetter.
func (m *Mount) SetSiteElevation(meters float64) error { return nil }

// --- vendor telemetry --------------------------------------------------------

// Voltage returns the input voltage (:Cv#).
func (m *Mount) Voltage() (float64, error) {
	s, err := m.get(":Cv#", ":Cv")
	if err != nil {
		return 0, err
	}
	var v float64
	_, err = fmt.Sscanf(s, "%f", &v)
	return v, err
}

// --- helpers ----------------------------------------------------------------

func hms(hours float64) (h, m int, s float64) {
	t := hours * 3600
	h = int(t) / 3600
	m = (int(t) / 60) % 60
	s = t - float64(h*3600+m*60)
	return
}

func dmsParts(deg float64) (sign byte, d, m int, s float64) {
	sign = '+'
	if deg < 0 {
		sign, deg = '-', -deg
	}
	t := deg * 3600
	d = int(t) / 3600
	m = (int(t) / 60) % 60
	s = t - float64(d*3600+m*60)
	return
}

func ack(ok bool, err error) error {
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("rainbow: mount rejected command")
	}
	return nil
}

// Capabilities. RST overrides much of the core (its own dialect).
var (
	_ lx200.Mount      = (*Mount)(nil)
	_ lx200.Homer      = (*Mount)(nil)
	_ lx200.Parker     = (*Mount)(nil)
	_ lx200.PierSider  = (*Mount)(nil) // derived from :CG3#/:CY#
	_ lx200.Horizontal = (*Mount)(nil)
	_ lx200.Guider     = (*Mount)(nil) // PulseGuide overridden (RST has no :Mg)
	_ lx200.TrackRater = (*Mount)(nil) // overridden (:Ct? not :T?)
	_ lx200.SiteSetter = (*Mount)(nil)
	_ lx200.AxisMover  = (*Mount)(nil) // MoveAxis inherited, StopAxis overridden
)
