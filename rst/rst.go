// Package rst drives Rainbow Astro RST mounts over USB-serial.
package rst

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mikefsq/lx200"
	"github.com/mikefsq/lx200/serial"
)

// Rainbow USB-serial: FTDI, 115200 baud.
const baud = 115200

// commandTimeout bounds a normal command. probeTimeout bounds the single identity read of a
// candidate port during Find, and is short because a port that is not a mount costs that long.
const (
	commandTimeout = 3 * time.Second
	probeTimeout   = 700 * time.Millisecond
	// slewFaultWindow is how long a slew waits to be refused before it is taken as accepted.
	// Every accepted slew pays it in full, so it only covers the round trip.
	slewFaultWindow = 400 * time.Millisecond
	// syncReplyWindow is how long an alignment command waits for the mount's CM verdict.
	// Silence is not an error: the frame can arrive later, and applyToken routes it.
	syncReplyWindow = 400 * time.Millisecond
)

// How long a read waits for a pushed completion token, and how often, so that a burst of
// getters costs one peek.
const (
	peekTimeout = 120 * time.Millisecond
	peekTTL     = 100 * time.Millisecond
)

// Mount is a Rainbow Astro RST mount on a LX200 connection.
type Mount struct {
	*lx200.Conn

	mu        sync.Mutex
	slewing   bool      // set on goto/home/park; cleared by the completion token
	lastPeek  time.Time // coalesces token peeks across a poll burst
	lastFault string
	targetRA  float64 // remembered for the degrees-based :Ck sync
	targetDec float64
	pulsing   bool

	homeFound bool         // a home seek has completed since power-on (gates slews)
	home      homePosition // West-horizon home coords, captured on home completion
	siteLat   float64      // cached site latitude (for the pole/park position)
	siteLatOK bool
	unparked  bool // Unpark was called explicitly; AlpacaAtPark honours it even at the pole
	parking   bool // a Park slew is in flight; its completion arms parkStopPending
	moving    bool // a continuous MoveAxis/Move is running; the mount does not report these

	homeCapturePending bool // a :CHO arrived; capture the home coords at the next safe read
	parkStopPending    bool // a park slew finished; stop tracking at the next safe read
}

// homePosition is the position recorded when a FindHome completes. RA and Dec are the values
// at that instant and drift with sidereal time thereafter.
type homePosition struct {
	valid            bool
	ra, dec, az, alt float64
}

// Open connects over USB-serial at 115200 baud.
func Open(portName string) (*Mount, error) {
	m, err := openRaw(portName, commandTimeout)
	if err != nil {
		return nil, err
	}
	m.init()
	return m, nil
}

// openRaw opens the port without talking to the mount, so a probe can use a short timeout and
// skip the seed reads in init.
func openRaw(portName string, timeout time.Duration) (*Mount, error) {
	tr, err := serial.Open(portName, baud)
	if err != nil {
		return nil, err
	}
	return &Mount{Conn: lx200.New(tr, timeout)}, nil
}

// init selects the comm mode and dialect and seeds the cached state. Best-effort: a write
// error here surfaces on the first real command rather than failing the open.
func (m *Mount) init() {
	m.selectDialect()
	// :GH# survives a driver reconnect, so a home done from the handset or a previous session
	// counts and no re-home is forced.
	if h, err := m.homeLatched(); err == nil && h {
		m.mu.Lock()
		m.homeFound = true
		m.mu.Unlock()
	}
	// Cached so the pole position does not re-read :Gt# on every poll.
	if lat, err := m.SiteLatitude(); err == nil {
		m.mu.Lock()
		m.siteLat, m.siteLatOK = lat, true
		m.mu.Unlock()
	}
}

// selectDialect puts the mount into USB comm mode and the Rainbow dialect, then clears the
// buffer. Separate from init because a probe needs it before it can expect a well-formed
// reply.
func (m *Mount) selectDialect() {
	// Avoid redundant :AU#/:AR# commands: each writes the configuration EEPROM.
	if m.inRainbowMode() {
		m.drainStale()
		return
	}
	_ = m.Blind(":AU#") // select USB comm mode (:AW# is WiFi)
	_ = m.Blind(":AR#") // select the Rainbow LX200 dialect
	m.drainStale()
}

// inRainbowMode reports whether the mount already echoes the command prefix, the flag :AR#
// sets and the one this package's reply matching depends on.
func (m *Mount) inRainbowMode() bool {
	s, err := m.Get(":GR#")
	return err == nil && strings.HasPrefix(s, ":GR")
}

// Park requires Dec near 90 degrees and RA near zero in the :CY# axis readings.
const (
	parkDecAxisMin = 89.9
	parkDecAxisMax = 90.0
	parkRAAxis     = 0.0
	parkRAAxisTol  = 0.1
)

// homeDecAxis and homeRAAxis are the mechanical axis angles at the mount's home, as reported by
// :CY#. A completed home seek leaves both at zero.
const (
	homeDecAxis = 0.0
	homeRAAxis  = 0.0
)

// Match tolerances for the home axis angles, in degrees of mechanical rotation. The Dec axis is
// held tight: the seek drives it onto a sensor and it reads a clean 0.00. The RA axis is given
// more room for settling and for the coarser repeatability of the larger axis.
const (
	homeDecAxisTolDeg = 1.0
	homeRAAxisTolDeg  = 5.0
)

// axisNear reports whether a mechanical axis angle is within tol degrees of want, compared the
// short way round. :CY# reports a signed angle that wraps, so -0.00 and 359.99 are 0.01 apart
// rather than 359.99.
func axisNear(got, want, tol float64) bool {
	d := math.Mod(math.Abs(got-want), 360)
	if d > 180 {
		d = 360 - d
	}
	return d <= tol
}

// PolePosition returns the celestial pole in horizon coordinates: Az 0 and Alt = latitude in
// the northern hemisphere, Az 180 and Alt = |latitude| in the southern.
func (m *Mount) PolePosition() (az, alt float64, err error) { return m.polePosition() }

// polePosition returns the celestial pole from the cached site latitude.
func (m *Mount) polePosition() (az, alt float64, err error) {
	m.mu.Lock()
	lat, ok := m.siteLat, m.siteLatOK
	m.mu.Unlock()
	if !ok {
		if lat, err = m.SiteLatitude(); err != nil {
			return 0, 0, err
		}
		m.mu.Lock()
		m.siteLat, m.siteLatOK = lat, true
		m.mu.Unlock()
	}
	if lat < 0 {
		return 180, -lat, nil
	}
	return 0, lat, nil
}

// homeLatched reads the :GH# home-established latch, which reads O once the mount has homed
// since power-on and stays O after it slews away.
func (m *Mount) homeLatched() (bool, error) {
	s, err := m.get(":GH#", ":GH")
	if err != nil {
		return false, err
	}
	return strings.HasPrefix(s, "O"), nil
}

// HomeFound reports whether a home seek has completed since power-on. Slews are gated on it,
// because the mount needs a mechanical reference before a goto means anything.
func (m *Mount) HomeFound() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.homeFound }

// HomePosition returns the coordinates recorded at the last successful home, with ok false if
// none was recorded this session. Diagnostic only; AtHome compares axis angles instead.
func (m *Mount) HomePosition() (ra, dec, az, alt float64, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h := m.home
	return h.ra, h.dec, h.az, h.alt, h.valid
}

// requireHome gates a slew on HomeFound, returning a clear error when the mount has
// not been homed since power-on.
func (m *Mount) requireHome(op string) error {
	if m.HomeFound() {
		return nil
	}
	return fmt.Errorf("rainbow: %s requires the mount to be homed first — run FindHome (the mount seeks its West-horizon mechanical home)", op)
}

// drainStale consumes completion tokens left over from a slew or home that finished after a
// previous session disconnected. Unread, the first would be returned as the reply to this
// session's first command.
func (m *Mount) drainStale() {
	for i := 0; i < 8; i++ {
		if _, err := m.Await(150 * time.Millisecond); err != nil {
			return
		}
	}
}

// Find auto-detects the RST by its FTDI USB id (0403:6001) and opens the first match. On
// macOS, where the port enumerator reports no VID or PID, it falls back to the FTDI VCP name
// convention, which is less selective.
func Find() (*Mount, error) {
	m, _, err := FindMatching(Filter{})
	return m, err
}

// Filter restricts discovery to a USB serial number when specified.
// Silent ports are not excluded from later scans: a mount may still be starting.
type Filter struct {
	Serial string // bind only this USB bridge serial; empty = any candidate
}

// Report is what a search learned, for a caller that wants to remember it.
type Report struct {
	Serial string // the USB bridge serial the mount was found on ("" if unknown)
}

// FindMatching opens the RST that satisfies f, and reports what it learned on the way.
func FindMatching(f Filter) (*Mount, Report, error) {
	var rep Report
	ports, err := serial.List()
	if err != nil {
		return nil, rep, err
	}
	cands := candidates(ports, f)
	if len(cands) == 0 {
		if f.Serial != "" {
			return nil, rep, fmt.Errorf("rainbow: no port with USB serial %q (FTDI 0403:6001)", f.Serial)
		}
		return nil, rep, fmt.Errorf("rainbow: no RST mount found (FTDI 0403:6001)")
	}
	for _, c := range cands {
		if probeRST(c.Name) {
			rep.Serial = c.SerialNumber
			m, err := Open(c.Name)
			return m, rep, err
		}
	}
	return nil, rep, fmt.Errorf("rainbow: no RST mount answered on %d candidate port(s) (FTDI 0403:6001)", len(cands))
}

// probeRST reports whether an RST answers on portName.
func probeRST(portName string) bool {
	m, err := openRaw(portName, probeTimeout)
	if err != nil {
		return false // busy (another driver holds it) or not openable
	}
	defer m.Close()
	// An RST already in the Rainbow dialect answers :AV# without anything being written to it,
	// which matters because this runs against ports belonging to other instruments.
	if _, err := m.Version(); err == nil {
		return true
	}
	m.selectDialect()
	_, err = m.Version()
	return err == nil
}

// findPort picks the RST's serial port from an enumerated list, or returns "" if nothing
// matches. Split out from Find so it can be tested without opening a port.
func findPort(ports []serial.PortInfo) string {
	if c := candidatePorts(ports); len(c) > 0 {
		return c[0]
	}
	return ""
}

// candidatePorts returns every port that could be an RST, best first. Which of several
// identical FTDI bridges is the mount cannot be told from the descriptor, so Find asks each.
func candidatePorts(ports []serial.PortInfo) []string {
	var out []string
	for _, p := range candidates(ports, Filter{}) {
		out = append(out, p.Name)
	}
	return out
}

// candidates returns the ports worth asking, best first, after applying f.
func candidates(ports []serial.PortInfo, f Filter) []serial.PortInfo {
	keep := func(p serial.PortInfo) bool {
		if f.Serial == "" {
			return true
		}
		sn := strings.ToLower(strings.TrimSpace(p.SerialNumber))
		return sn == strings.ToLower(strings.TrimSpace(f.Serial))
	}
	var out []serial.PortInfo
	for _, p := range ports {
		if p.IsUSB && p.VID == "0403" && p.PID == "6001" && keep(p) {
			out = append(out, p)
		}
	}
	for _, p := range ports {
		if p.VID == "" && p.IsUSB && strings.Contains(p.Name, "usbserial") && keep(p) {
			out = append(out, p)
		}
	}
	return out
}

// Version returns the firmware version (:AV#).
func (m *Mount) Version() (string, error) { return m.get(":AV#", ":AV") }

// drainToken peeks for a pushed completion token, but only while a move is in flight and at
// most once per peekTTL. It runs before a coordinate read so the token is not mistaken for the
// reply.
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
	m.applyToken(tok)
}

// applyToken routes a completion or fault token to the slew, home and park state. Work the
// token implies is deferred to the next safe read, because whichever path caught it may be
// mid-read. Tolerant of a leading ':'.
func (m *Mount) applyToken(tok string) {
	t := strings.TrimPrefix(tok, ":")
	m.mu.Lock()
	defer m.mu.Unlock()
	// A sync or added alignment point answers with a CM frame; a leading F is a rejection.
	if strings.HasPrefix(t, "CM") {
		if strings.HasPrefix(t, "CMF") {
			m.lastFault = tok
		}
		return
	}
	switch t {
	case "MM0": // slew complete
		m.slewing = false
		if m.parking { // the park has arrived — stop the tracking :MS# turned on
			m.parking = false
			m.parkStopPending = true
		}
	case "CHO": // home complete — latch home-found; capture the coords shortly
		m.slewing = false
		m.homeFound = true
		m.homeCapturePending = true
	case "MML", "MMU", "MME", "CH0", "CH<": // faults (limit / home fail)
		m.slewing = false
		m.lastFault = tok
	}
}

// maybeCaptureHome records the home position if a :CHO completion armed it. It runs at the top
// of get, and clears the flag first so its own reads do not recurse.
func (m *Mount) maybeCaptureHome() {
	m.mu.Lock()
	pending := m.homeCapturePending
	m.homeCapturePending = false
	m.mu.Unlock()
	if pending {
		m.captureHome()
	}
}

// maybeFinishPark stops tracking once a park slew has arrived, since :MS# starts tracking when
// it completes. Park returns as soon as the goto is accepted, so there is no caller left to do
// it; the work is deferred to the next safe read.
func (m *Mount) maybeFinishPark() {
	m.mu.Lock()
	pending := m.parkStopPending
	m.parkStopPending = false
	m.mu.Unlock()
	if pending {
		_ = m.SetTracking(false)
	}
}

// captureHome records the current position as the West-horizon home.
func (m *Mount) captureHome() {
	ra, e1 := m.RA()
	dec, e2 := m.Dec()
	az, e3 := m.Azimuth()
	alt, e4 := m.Altitude()
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
		return
	}
	m.mu.Lock()
	m.home = homePosition{valid: true, ra: ra, dec: dec, az: az, alt: alt}
	m.mu.Unlock()
}

func (m *Mount) setSlewing(v bool) {
	m.mu.Lock()
	m.slewing = v
	m.lastPeek = time.Time{}
	if v {
		m.lastFault = ""             // a fresh slew/home clears the previous outcome
		m.unparked = false           // ... and retires an explicit Unpark (see AlpacaAtPark)
		m.homeCapturePending = false // ... and a pending home capture (no longer at home)
		m.parking = false            // ... and cancels a park in flight (Park re-arms it)
		m.parkStopPending = false    // ... and the tracking-off that would have followed it
	}
	m.mu.Unlock()
}

// Fault returns the last motion-abort token, or "" if the last move completed cleanly. :MML
// and :MMU are limit violations, :MME an error, :CH0 and :CH< a home failure.
//
// It is cleared when the next move starts, so a non-empty Fault once Slewing goes false means
// the move aborted rather than arrived. This is the only way to detect that after an
// asynchronous slew.
func (m *Mount) Fault() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastFault
}

// get sends a query and strips the echoed command prefix from the reply
// (:GR20:28:56.9# -> "20:28:56.9").
func (m *Mount) get(cmd, prefix string) (string, error) {
	m.maybeCaptureHome() // capture a home deferred by an earlier :CHO, before this read
	m.maybeFinishPark()  // ... and stop tracking if a park has just arrived
	match := func(r string) bool { return strings.HasPrefix(r, prefix) }
	// One retry. A late completion token can crowd the real reply past GetMatching's skip
	// budget and surface as ErrNoMatch. The tokens are transient, so draining and re-issuing
	// once clears them, so a cold first read is as reliable as a warm one.
	for attempt := 0; attempt < 2; attempt++ {
		s, err := m.GetMatching(cmd, match, m.applyToken, 3)
		if err == nil {
			return strings.TrimPrefix(s, prefix), nil
		}
		if !errors.Is(err, lx200.ErrNoMatch) {
			return "", err
		}
		m.drainStale()
	}
	return "", fmt.Errorf("rainbow: %s: no matching reply after retry: %w", cmd, lx200.ErrNoMatch)
}

func (m *Mount) coord(cmd, prefix string) (float64, error) {
	m.drainToken()
	s, err := m.get(cmd, prefix)
	if err != nil {
		return 0, err
	}
	return lx200.ParseSexagesimal(s)
}

// RA reads the current right ascension in hours (:GR#).
func (m *Mount) RA() (float64, error) { return m.coord(":GR#", ":GR") }

// Dec reads the current declination in degrees (:GD#).
func (m *Mount) Dec() (float64, error) { return m.coord(":GD#", ":GD") }

// Altitude reads the current altitude above the horizon in degrees (:GA#).
func (m *Mount) Altitude() (float64, error) { return m.coord(":GA#", ":GA") }

// Azimuth reads the current azimuth in degrees, East of North (:GZ#).
func (m *Mount) Azimuth() (float64, error) { return m.coord(":GZ#", ":GZ") }

// SetTargetRA sets the goto and sync target right ascension in hours (:Sr#), remembering it
// for the degrees-based :Ck sync. Reports whether the mount accepted it.
func (m *Mount) SetTargetRA(hours float64) (bool, error) {
	m.mu.Lock()
	m.targetRA = hours
	m.mu.Unlock()
	h, mm, s := hms(hours)
	return m.Ack(fmt.Sprintf(":Sr%02d:%02d:%04.1f#", h, mm, s))
}

// SetTargetDec sets the goto and sync target declination in degrees (:Sd#), as SetTargetRA.
func (m *Mount) SetTargetDec(deg float64) (bool, error) {
	m.mu.Lock()
	m.targetDec = deg
	m.mu.Unlock()
	sign, d, mm, s := dmsParts(deg)
	return m.Ack(fmt.Sprintf(":Sd%c%02d*%02d'%04.1f#", sign, d, mm, s))
}

// SlewToTarget starts an equatorial goto (:MS#). Completion arrives later as a :MM0# token.
// Gated on HomeFound, and note that the goto leaves the mount tracking at the target.
func (m *Mount) SlewToTarget() error {
	if err := m.requireHome("goto"); err != nil {
		return err
	}
	if err := m.slewCmd(":MS#", "MS"); err != nil {
		return err
	}
	m.setSlewing(true)
	return nil
}

// slewCmd sends a slew command and checks for a refusal before latching motion.
func (m *Mount) slewCmd(cmd, verb string) error {
	reply, err := m.SlewNack(cmd, slewFaultWindow)
	if err != nil {
		return err
	}
	if reply == "" {
		return nil // silence: accepted
	}
	if t := strings.TrimPrefix(reply, ":"); strings.HasPrefix(t, verb) {
		return fmt.Errorf("rainbow: slew refused: %s", t)
	}
	m.applyToken(reply) // an unsolicited completion/fault token from an earlier move
	return nil
}

// SlewToAltAz slews to a horizontal target: :Sz# and :Sa# to set it, then :MA#. Azimuth is
// degrees east of north. The mount does not ack :Sz# or :Sa#, so they go blind; completion
// arrives as a pushed token.
//
// This works whatever the mount's internal Equat or AltAz mode, which is a handset setting and
// is not visible on the wire.
func (m *Mount) SlewToAltAz(azDeg, altDeg float64) error {
	if err := m.requireHome("alt/az goto"); err != nil {
		return err
	}
	for azDeg >= 360 {
		azDeg -= 360
	}
	for azDeg < 0 {
		azDeg += 360
	}
	aD := int(azDeg)
	aM := int((azDeg - float64(aD)) * 60)
	aS := (azDeg-float64(aD))*60 - float64(aM)
	if err := m.Blind(fmt.Sprintf(":Sz%03d*%02d'%04.1f#", aD, aM, aS*60)); err != nil {
		return err
	}
	sign, d, mm, s := dmsParts(altDeg)
	if err := m.Blind(fmt.Sprintf(":Sa%c%02d*%02d'%04.1f#", sign, d, mm, s)); err != nil {
		return err
	}
	// :MA# answers as :MS# does, only to refuse, so it is read rather than fired blind.
	if err := m.slewCmd(":MA#", "MA"); err != nil {
		return err
	}
	m.setSlewing(true)
	return nil
}

// SlewToPole points the tube at the celestial pole for polar alignment, as an alt/az goto.
// Alt/az on purpose: the pole is Dec 90, where RA is degenerate.
//
// This is for sighting the pole and adjusting the mount's physical alt/az. It is not the park
// position, which needs the RA axis pinned as well; see Park.
func (m *Mount) SlewToPole() error {
	if err := m.requireHome("polar-axis slew"); err != nil {
		return err
	}
	az, alt, err := m.polePosition()
	if err != nil {
		return err
	}
	return m.SlewToAltAz(az, alt)
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

// SyncToTarget re-centers on the remembered target (:Ck#). Gated on HomeFound: syncing an
// un-homed mount corrupts its coordinate model.
func (m *Mount) SyncToTarget() (string, error) {
	if err := m.requireHome("sync"); err != nil {
		return "", err
	}
	return m.syncAck(m.syncCmd(":Ck"))
}

// syncAck sends an alignment command and waits briefly for the mount's CM verdict. Silence
// means the verdict has not arrived yet, not that anything failed.
func (m *Mount) syncAck(cmd string) (string, error) {
	reply, err := m.SlewNack(cmd, syncReplyWindow)
	if err != nil {
		return "", err
	}
	t := strings.TrimPrefix(reply, ":")
	if t == "" {
		return "", nil // no verdict yet; applyToken will route it when it lands
	}
	if !strings.HasPrefix(t, "CM") {
		m.applyToken(t) // an unrelated token overtook the verdict
		return "", nil
	}
	if body := strings.TrimPrefix(t, "CM"); strings.HasPrefix(body, "F") {
		return "", fmt.Errorf("rainbow: alignment refused: %s", t)
	}
	return strings.TrimPrefix(t, "CM"), nil
}

// AddAlignmentPoint syncs to the remembered target and saves it as a star-alignment point
// (:CN#), for building a multi-point model. Set the target first.
func (m *Mount) AddAlignmentPoint() error {
	if err := m.requireHome("add alignment point"); err != nil {
		return err
	}
	_, err := m.syncAck(m.syncCmd(":CN"))
	return err
}

// Halt stops motion (:Q#) and ends both the goto and the continuous-move state.
//
// Refused while a home seek is running: :Q# sets the flag that seek is polling, and interrupting
// it can leave the mount's homing guard set until a power cycle. See refuseWhileHoming. A seek
// runs at a fixed rate and ends by itself; there is no safe way to cut it short over the wire.
func (m *Mount) Halt() error {
	if err := m.refuseWhileHoming("halt"); err != nil {
		return err
	}
	if err := m.Blind(":Q#"); err != nil {
		return err
	}
	m.setSlewing(false)
	m.setMoving(false)
	return nil
}

// Slewing reports whether the mount is moving under a goto, a home seek, a park or a
// continuous MoveAxis.
//
// The mount only tracks the first three. A manual move sets nothing on the wire, so this ORs in
// a local flag the move commands maintain. Pulse guiding is excluded, as ASCOM requires;
// IsPulseGuiding reports that instead.
//
// Slewing going false does not mean the move arrived. Check Fault.
func (m *Mount) Slewing() (bool, error) {
	m.drainToken()
	// Polling Slewing is how a caller waits out a park, and it does not go through get, so the
	// deferred tracking-off runs from here as well.
	m.maybeFinishPark()
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.slewing || m.moving, nil
}

// setMoving latches a continuous move. Pulse guiding uses the same wire commands but must not
// set it; see Slewing.
func (m *Mount) setMoving(v bool) {
	m.mu.Lock()
	m.moving = v
	m.mu.Unlock()
}

// Tracking reads :AT# (-> :AT0# / :AT1#).
func (m *Mount) Tracking() (bool, error) {
	s, err := m.get(":AT#", ":AT")
	if err != nil {
		return false, err
	}
	return strings.HasPrefix(s, "1"), nil
}

// track sends a tracking command and consumes its echo. These are not blind: the mount echoes
// the command back with the case flipped, and leaving that unread desyncs every later query.
func (m *Mount) track(cmd string) error {
	_, err := m.Get(cmd)
	return err
}

// SetTracking enables (:CtA#) / disables (:CtL#) tracking.
func (m *Mount) SetTracking(on bool) error {
	cmd := ":CtL#"
	if on {
		cmd = ":CtA#"
	}
	return m.track(cmd)
}

// TrackSidereal selects the sidereal tracking rate (:CtR#). Note that lunar is :CtM#, not
// :CtL#, which disables tracking.
func (m *Mount) TrackSidereal() error { return m.track(":CtR#") }

// TrackSolar selects the solar tracking rate (:CtS#).
func (m *Mount) TrackSolar() error { return m.track(":CtS#") }

// TrackLunar selects the lunar tracking rate (:CtM#).
func (m *Mount) TrackLunar() error { return m.track(":CtM#") }

// TrackMode is the active tracking rate reported by :Ct?#.
type TrackMode int

// Tracking rates reported by :Ct?# (-> :CT0#..:CT3#).
const (
	TrackModeSidereal TrackMode = iota // :CtR# (:Ct?# -> :CT0#)
	TrackModeSolar                     // :CtS# (:Ct?# -> :CT1#)
	TrackModeLunar                     // :CtM# (:Ct?# -> :CT2#)
	TrackModeCustom                    // :CtU# (:Ct?# -> :CT3#)
)

// TrackMode reads the active tracking rate (:Ct?#).
func (m *Mount) TrackMode() (TrackMode, error) {
	s, err := m.get(":Ct?#", ":CT")
	if err != nil {
		return 0, err
	}
	switch {
	case strings.HasPrefix(s, "3"):
		return TrackModeCustom, nil
	case strings.HasPrefix(s, "2"):
		return TrackModeLunar, nil
	case strings.HasPrefix(s, "1"):
		return TrackModeSolar, nil
	default:
		return TrackModeSidereal, nil
	}
}

// ErrHoming is returned by an operation refused because a home seek is already running.
var ErrHoming = errors.New("rainbow: a home seek is already running")

// FindHome starts a mechanical home seek after stopping tracking.
// Poll Slewing for completion; coordinates stay near the starting position
// during the seek. A :CHO# token latches HomeFound. Re-entry is refused.
func (m *Mount) FindHome() error {
	// Read before writing. A stuck busy flag is the case this catches, and it is not
	// recoverable over the wire, so saying so plainly beats a three-minute silent wait.
	busy, err := m.Homing()
	if err != nil {
		return fmt.Errorf("rainbow: cannot tell whether a home seek is running: %w", err)
	}
	if busy {
		return fmt.Errorf("%w (:AH1); it clears when the seek returns, and a seek interrupted "+
			"part-way can leave it set until the mount is power-cycled", ErrHoming)
	}
	// With tracking on, :Ch# aborts and pushes a :CH< fail token. Best-effort; this also
	// consumes the :CTL# echo.
	_ = m.track(":CtL#")
	if err := m.Blind(":Ch#"); err != nil {
		return err
	}
	m.setSlewing(true)
	return nil
}

// refuseWhileHoming rejects commands that could interrupt a home seek and leave
// the firmware busy until power-cycled. A failed status read does not block a halt.
func (m *Mount) refuseWhileHoming(op string) error {
	busy, err := m.Homing()
	if err != nil || !busy {
		return nil
	}
	return fmt.Errorf("rainbow: %s refused: %w — interrupting a seek can wedge the mount's "+
		"homing flag until it is power-cycled; wait for it to finish", op, ErrHoming)
}

// AtHome reports whether the mount is at its mechanical home, by reading the axis angles from
// :CY# and requiring both to be zero. It is false until the mount has homed this power-cycle,
// because the angles have no reference until then.
func (m *Mount) AtHome() (bool, error) {
	if !m.HomeFound() {
		return false, nil
	}
	decAxis, raAxis, err := m.AxisAngles()
	if err != nil {
		return false, err
	}
	return axisNear(decAxis, homeDecAxis, homeDecAxisTolDeg) &&
		axisNear(raAxis, homeRAAxis, homeRAAxisTolDeg), nil
}

// Homing reports whether a home seek is running (:AH#).
// Use AtHome for position and HomeFound for a completed home seek.
func (m *Mount) Homing() (bool, error) {
	s, err := m.get(":AH#", ":AH")
	if err != nil {
		return false, err
	}
	return strings.HasPrefix(s, "1"), nil
}

// parkHourAngle is the hour angle Park drives to. +6h puts the RA axis at mechanical zero,
// which is where the handset's polar-axis parking preset lands, tube on top.
const parkHourAngle = 6.0

// parkDec is the declination Park targets, one arcminute short of the pole. Not 90: at exactly
// Dec 90 the RA coordinate is degenerate and the goto has nothing to pin the RA axis to. One
// arcminute is the smallest margin that survives the mount's own rounding.
const parkDec = 89 + 59.0/60.0

// Park stows the mount along its polar axis and stops tracking on arrival.
// An hour-angle target pins the RA axis; tracking must be stopped again because
// an equatorial slew enables it. A mount already parked is stopped without a
// slew, since a zero-distance move may produce no completion token.
func (m *Mount) Park() error {
	if err := m.requireHome("park"); err != nil {
		return err
	}
	if err := m.refuseWhileHoming("park"); err != nil {
		return err
	}
	_ = m.SetTracking(false) // stow with tracking off (like FindHome)
	// Checked AFTER tracking is stopped, because tracking turns the RA axis and this reads the
	// axis angles. An error here is not fatal: fall through and slew, which is always correct.
	if at, err := m.AtPark(); err == nil && at {
		m.mu.Lock()
		m.unparked = false // nothing to move; the state is all that changes
		m.mu.Unlock()
		return nil
	}
	// The target is a MOUNT ORIENTATION expressed in the sky frame, so it has to be recomputed
	// from the clock every time: RA = LST - 6h is a different RA every park, and the one thing
	// that stays fixed is the hour angle, and the axis follows the hour angle.
	lst, err := m.SiderealTime()
	if err != nil {
		return fmt.Errorf("rainbow: park needs the mount's sidereal time to place the RA axis: %w", err)
	}
	ra := math.Mod(lst-parkHourAngle+24, 24)
	if ok, err := m.SetTargetRA(ra); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("rainbow: park: mount rejected target RA %.4fh", ra)
	}
	if ok, err := m.SetTargetDec(parkDec); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("rainbow: park: mount rejected target Dec %.4f", parkDec)
	}
	if err := m.SlewToTarget(); err != nil {
		return err
	}
	// Arm the tracking-off. It cannot be done now: the goto is still in flight, and :MS# turns
	// tracking ON when it arrives. See maybeFinishPark.
	m.mu.Lock()
	m.parking = true
	m.mu.Unlock()
	return nil
}

// SiteLatitude reads the mount's configured latitude (:Gt# -> :Gt+37*30'00#).
func (m *Mount) SiteLatitude() (float64, error) {
	s, err := m.get(":Gt#", ":Gt")
	if err != nil {
		return 0, err
	}
	return lx200.ParseSexagesimal(s)
}

// Unpark clears the parked state.
func (m *Mount) Unpark() error {
	m.mu.Lock()
	m.unparked = true
	m.mu.Unlock()
	return nil
}

// AtPark reports whether the mount is stowed along its polar axis, by reading the mechanical
// axis angles from :CY# and requiring the park signature. See parkDecAxisMin.
func (m *Mount) AtPark() (bool, error) {
	if !m.HomeFound() {
		return false, nil
	}
	decAxis, raAxis, err := m.AxisAngles()
	if err != nil {
		return false, err
	}
	return decAxis >= parkDecAxisMin && decAxis <= parkDecAxisMax &&
		axisNear(raAxis, parkRAAxis, parkRAAxisTol), nil
}

// AxisAngles returns mechanical Dec-axis and RA-axis angles in degrees (:CY#).
// These distinguish orientations at the celestial pole, where RA is degenerate.
func (m *Mount) AxisAngles() (decAxis, raAxis float64, err error) {
	raw, err := m.get(":CY#", ":CY") // "+089.49/-000.00" -> DEC / RA
	if err != nil {
		return 0, 0, err
	}
	i := strings.IndexByte(raw, '/')
	if i < 0 {
		return 0, 0, fmt.Errorf("rainbow: :CY#: no '/' in %q", raw)
	}
	if _, err := fmt.Sscanf(raw[:i], "%f", &decAxis); err != nil {
		return 0, 0, fmt.Errorf("rainbow: :CY# dec axis %q: %w", raw[:i], err)
	}
	if _, err := fmt.Sscanf(raw[i+1:], "%f", &raAxis); err != nil {
		return 0, 0, fmt.Errorf("rainbow: :CY# ra axis %q: %w", raw[i+1:], err)
	}
	return decAxis, raAxis, nil
}

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

// PulseGuide guides for ms milliseconds. The mount has no :Mg#, so this selects the custom
// track and guide rates, starts a move, and stops it asynchronously. Returns immediately.
func (m *Mount) PulseGuide(d lx200.Direction, ms int) error {
	if err := m.track(":CtU#"); err != nil { // custom (guide) tracking (echoes :CTU#)
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

// IsPulseGuiding reports whether a PulseGuide is currently in progress.
func (m *Mount) IsPulseGuiding() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pulsing
}

// StopAxis stops the given axis. The mount has no directional quit, only the bare :Q#.
func (m *Mount) StopAxis(a lx200.Axis) error {
	m.setMoving(false)
	return m.Blind(":Q#")
}

// SetSiteLatitude sets the observing latitude in degrees (:St#). Blind; read it back with
// SiteLatitude.
func (m *Mount) SetSiteLatitude(deg float64) error {
	sign, d, mm, s := dmsParts(deg)
	return m.Blind(fmt.Sprintf(":St%c%02d*%02d'%02d#", sign, d, mm, int(s)))
}

// SetSiteLongitude sets the observing longitude in degrees, east-positive (:Sg#). The mount is
// east-negative on the wire, so the value is negated. Blind; read it back with SiteLongitude.
func (m *Mount) SetSiteLongitude(deg float64) error {
	sign, d, mm, s := dmsParts(-deg) // negate: RST East = negative
	return m.Blind(fmt.Sprintf(":Sg%c%03d*%02d'%02d#", sign, d, mm, int(s)))
}

// SetSiteElevation does nothing. The protocol has no site-elevation command; the method exists
// to satisfy lx200.SiteSetter.
func (m *Mount) SetSiteElevation(meters float64) error { return nil }

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

// SiderealTime reads local sidereal time in hours (:GS#).
func (m *Mount) SiderealTime() (float64, error) { return m.coord(":GS#", ":GS") }

// LocalTime reads the mount's clock in hours since midnight (:GL#). This is local time; the
// mount's :GG# offset turns it into UTC. Sidereal time, and so the hour angle of every goto,
// is derived from the pair.
func (m *Mount) LocalTime() (float64, error) { return m.coord(":GL#", ":GL") }

// Date reads the calendar date as "MM/DD/YY" (:GC#). This is the mount's local date, not the
// UTC one, and the two disagree for part of every day.
func (m *Mount) Date() (string, error) { return m.get(":GC#", ":GC") }

// dateReplyWindow bounds each extra frame :SC# emits after its ack byte. Short, because a
// mount that does not send them must not cost a full command timeout twice.
const dateReplyWindow = 400 * time.Millisecond

// UTCOffset reads the timezone offset in hours (:GG#), in the LX200 convention: hours to add
// to local time to reach UTC, so +7 means local is UTC-7.
func (m *Mount) UTCOffset() (float64, error) {
	s, err := m.get(":GG#", ":GG")
	if err != nil {
		return 0, err
	}
	var v float64
	_, err = fmt.Sscanf(s, "%f", &v)
	return v, err
}

// SetLocalTime sets the mount's clock from t's wall-clock time (:SL#). Only the time of day is
// sent and t's zone is ignored, because the mount holds local time and owns its offset
// separately. Blind; read it back with LocalTime.
func (m *Mount) SetLocalTime(t time.Time) error { return m.Blind(t.Format(":SL15:04:05#")) }

// SetUTCOffset sets the offset added to local time to obtain UTC (:SG#), so positive west of
// Greenwich. Whole hours only: the mount has no minutes field, and truncating a half-hour zone
// would produce the class of clock error this exists to fix.
func (m *Mount) SetUTCOffset(offset time.Duration) error {
	if offset%time.Hour != 0 {
		return fmt.Errorf("rainbow: UTC offset %v is not a whole number of hours; the RST has no minutes field", offset)
	}
	sign := byte('+')
	if offset < 0 {
		sign, offset = '-', -offset
	}
	return m.Blind(fmt.Sprintf(":SG%c%02d#", sign, int(offset/time.Hour)))
}

// SetUTC sets local date/time using the mount's configured UTC offset.
// The date is written only when it changes to avoid recomputing the ephemeris.
func (m *Mount) SetUTC(t time.Time) error {
	off, err := m.UTCOffset()
	if err != nil {
		return fmt.Errorf("rainbow: cannot set the clock without the mount's UTC offset: %w", err)
	}
	local := t.UTC().Add(-time.Duration(off * float64(time.Hour)))
	if err := m.SetLocalTime(local); err != nil {
		return err
	}
	want := local.Format("01/02/06")
	got, err := m.Date()
	if err != nil {
		return fmt.Errorf("rainbow: clock set, but the date could not be checked: %w", err)
	}
	if strings.TrimSpace(got) == want {
		return nil // already correct — do not trigger the recompute
	}
	return m.SetDate(int(local.Month()), local.Day(), local.Year()%100)
}

// SiteLongitude reads the configured longitude in east-positive degrees (:Gg#). The mount is
// east-negative on the wire, so the parsed value is negated.
func (m *Mount) SiteLongitude() (float64, error) {
	s, err := m.get(":Gg#", ":Gg")
	if err != nil {
		return 0, err
	}
	deg, err := lx200.ParseSexagesimal(s)
	if err != nil {
		return 0, err
	}
	return -deg, nil
}

// SiteName reads one of the three stored site names, 1 to 3 (:GM#, :GN#, :GO#). The replies
// carry no echo prefix. Note that :GP# is the precision mode, not a fourth name.
func (m *Mount) SiteName(n int) (string, error) {
	cmds := map[int]string{1: ":GM#", 2: ":GN#", 3: ":GO#"}
	cmd, ok := cmds[n]
	if !ok {
		return "", fmt.Errorf("rainbow: site name index %d out of range 1..3", n)
	}
	s, err := m.Get(cmd)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(s), nil
}

// Precision reports the coordinate precision mode, H or L (:GP#), which governs how the target
// setters parse their arguments. :AR# forces high precision at connect.
func (m *Mount) Precision() (string, error) {
	s, err := m.Get(":GP#")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(s), nil
}

// SetPrecision selects high or low coordinate precision. Blind; read it back with Precision.
func (m *Mount) SetPrecision(high bool) error {
	if high {
		return m.Blind(":SPH#")
	}
	return m.Blind(":SPL#")
}

// EchoPrefix enables or disables the echoed command prefix on replies. Blind, and this driver
// depends on the echo for resynchronisation, so it should stay enabled.
func (m *Mount) EchoPrefix(on bool) error {
	if on {
		return m.Blind(":SPE#")
	}
	return m.Blind(":SPF#")
}

// SerialNumber reads the mount's own 6-digit serial number (:AS#). Unlike the FTDI bridge
// serial it identifies the mount rather than the adapter, so it survives a cable swap and is
// what to key persistent per-device state on.
func (m *Mount) SerialNumber() (string, error) { return m.get(":AS#", ":AS") }

// GearRatio reads the configured RA and Dec gear ratios (:AG#). Factory calibration, per-unit,
// and every rate and slew distance derives from them. The setter is in Unsafe.
func (m *Mount) GearRatio() (ra, dec int, err error) { return m.pairInts(":AG#", ":AG") }

// WormCount reads the configured RA and Dec worm counts (:AP#), as GearRatio.
func (m *Mount) WormCount() (ra, dec int, err error) { return m.pairInts(":AP#", ":AP") }

// pairInts reads a reply of the form "<n>*<n>" and returns both halves.
func (m *Mount) pairInts(cmd, prefix string) (a, b int, err error) {
	s, err := m.get(cmd, prefix)
	if err != nil {
		return 0, 0, err
	}
	f := strings.SplitN(s, "*", 2)
	if len(f) != 2 {
		return 0, 0, fmt.Errorf("rainbow: %s: cannot split %q on '*'", cmd, s)
	}
	if a, err = strconv.Atoi(strings.TrimSpace(f[0])); err != nil {
		return 0, 0, fmt.Errorf("rainbow: %s: %w", cmd, err)
	}
	if b, err = strconv.Atoi(strings.TrimSpace(f[1])); err != nil {
		return 0, 0, fmt.Errorf("rainbow: %s: %w", cmd, err)
	}
	return a, b, nil
}

// TargetRA reads back the goto target right ascension the mount holds (:Gr#). The horizontal
// setters are blind, so a readback is the only way to confirm a target was accepted.
func (m *Mount) TargetRA() (float64, error)  { return m.coord(":Gr#", ":Gr") }
func (m *Mount) TargetDec() (float64, error) { return m.coord(":Gd#", ":Gd") }

// TargetAltAz reads back the horizontal target set by SlewToAltAz.
func (m *Mount) TargetAltAz() (az, alt float64, err error) {
	if az, err = m.coord(":Gz#", ":Gz"); err != nil {
		return 0, 0, err
	}
	alt, err = m.coord(":Ga#", ":Ga")
	return az, alt, err
}

// SlewLimits reads the six axis-limit registers (:CA# to :CF#), in that order. The motion
// controller enforces them, and a goto that violates one is refused. How they map onto axis
// and bound is not established, so they are returned raw.
func (m *Mount) SlewLimits() ([6]float64, error) {
	var out [6]float64
	for i, c := range []string{"A", "B", "C", "D", "E", "F"} {
		s, err := m.get(":C"+c+"#", ":C"+c)
		if err != nil {
			return out, err
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil {
			return out, fmt.Errorf("rainbow: :C%s#: %w", c, err)
		}
		out[i] = v
	}
	return out, nil
}

// GPSState is the mount's GPS fix, the second flag of :GY#. It is tri-state rather than the
// O or X of the others. A mount reporting anything but GPSFix is running on its internal clock
// with nothing disciplining it.
type GPSState byte

const (
	GPSNone     GPSState = 'X' // no GPS
	GPSTimeOnly GPSState = 'T' // time acquired, no position fix
	GPSFix      GPSState = 'O' // full fix
)

func (g GPSState) String() string {
	switch g {
	case GPSNone:
		return "none"
	case GPSTimeOnly:
		return "time-only"
	case GPSFix:
		return "fix"
	}
	return "unknown(" + string(rune(g)) + ")"
}

type SystemStatus struct {
	TCS      bool // temperature-control / controller ok
	GPS      GPSState
	DecMotor bool
	RAMotor  bool
	Raw      string // the raw flag string, e.g. "OXOO"
}

// SystemStatus reads controller, GPS and motor health (:GY#).
func (m *Mount) SystemStatus() (SystemStatus, error) {
	s, err := m.get(":GY#", ":GY")
	if err != nil {
		return SystemStatus{}, err
	}
	ok := func(i int) bool { return i < len(s) && s[i] == 'O' }
	gps := GPSState('?')
	if len(s) > 1 {
		gps = GPSState(s[1])
	}
	return SystemStatus{TCS: ok(0), GPS: gps, DecMotor: ok(2), RAMotor: ok(3), Raw: s}, nil
}

// MotorLoad reads the per-axis motor load as a percentage (:CP#), Dec then RA.
func (m *Mount) MotorLoad() (decPct, raPct float64, err error) {
	s, err := m.get(":CP#", ":CP")
	if err != nil {
		return 0, 0, err
	}
	_, err = fmt.Sscanf(s, "%f|%f", &decPct, &raPct)
	return decPct, raPct, err
}

// AutoResume reports whether auto-resume is enabled (:CR#).
func (m *Mount) AutoResume() (bool, error) {
	s, err := m.get(":CR#", ":CR")
	if err != nil {
		return false, err
	}
	return strings.HasPrefix(s, "R"), nil
}

// GuideRate reads the guide rate as a multiple of sidereal (:CU0#).
func (m *Mount) GuideRate() (float64, error) {
	s, err := m.get(":CU0#", ":CU0=")
	if err != nil {
		return 0, err
	}
	var v float64
	_, err = fmt.Sscanf(s, "%f", &v)
	return v, err
}

// SetGuideRate sets the guide rate as a multiple of sidereal (:Cu0=#). Blind.
//
// This writes speed slot 0, which :RG# selects; it does not select it. The slot lives in the
// configuration EEPROM, so adjusting it every session spends a flash write cycle each time.
func (m *Mount) SetGuideRate(xSidereal float64) error {
	return m.Blind(fmt.Sprintf(":Cu0=%.1f#", xSidereal))
}

// Manual-slew rates in degrees per second, the four the vendor's driver advertises. The mount
// itself has no fixed set.
const (
	AxisRateMax    = 8.3333    // 2000x sidereal
	AxisRateFast   = 2.5       //  600x
	AxisRateMedium = 0.8333    //  200x
	AxisRateSlow   = 0.0041667 //    1x — sidereal
)

// siderealPerDegSec converts degrees per second to multiples of the sidereal rate.
const siderealPerDegSec = 3600.0 / 15.0

// AxisRates returns the manual-slew rates this driver offers, fastest first. SetAxisRate
// accepts any rate; these are the ones the vendor advertises.
func AxisRates() []float64 {
	return []float64{AxisRateMax, AxisRateFast, AxisRateMedium, AxisRateSlow}
}

// SetAxisRate selects the manual-slew rate in degrees per second.
//
// It writes the rate into a speed slot and then selects the preset that reads that slot. SetRate
// alone does not: a preset selects a slot without saying what is in it, and the slots hold
// whatever was last written to them by anything.
func (m *Mount) SetAxisRate(degPerSec float64) error {
	if degPerSec <= 0 {
		return m.Halt()
	}
	mult := int(degPerSec*siderealPerDegSec + 0.5)
	switch {
	case degPerSec >= AxisRateMax:
		// Sent as a literal, matching the vendor driver, so the fastest rate is byte-identical
		// to what the firmware is known to accept.
		if err := m.Blind(":Cu3=2000#"); err != nil {
			return err
		}
		return m.SetRate(lx200.RateMax)
	case degPerSec >= AxisRateFast:
		if err := m.SetSlewSpeed(3, mult); err != nil {
			return err
		}
		return m.SetRate(lx200.RateMax)
	case degPerSec >= AxisRateMedium:
		if err := m.SetSlewSpeed(2, mult); err != nil {
			return err
		}
		return m.SetRate(lx200.RateFind)
	case degPerSec >= AxisRateSlow:
		if err := m.SetSlewSpeed(1, mult); err != nil {
			return err
		}
		return m.SetRate(lx200.RateCenter)
	default:
		if err := m.SetGuideRate(degPerSec * siderealPerDegSec); err != nil {
			return err
		}
		return m.SetRate(lx200.RateGuide)
	}
}

// MoveAxis starts a continuous slew at one of the LX200 rate presets.
//
// A preset only selects a speed slot on this mount, it does not set one, so this programs the
// slot first. Use MoveAxisRate when the caller has a rate in degrees per second.
func (m *Mount) MoveAxis(a lx200.Axis, positive bool, rate lx200.Rate) error {
	return m.MoveAxisRate(a, positive, degPerSecForPreset(rate))
}

// MoveAxisRate starts a continuous slew at rate degrees per second, programming the speed
// slot before selecting it. A rate of zero stops the axis.
func (m *Mount) MoveAxisRate(a lx200.Axis, positive bool, degPerSec float64) error {
	if degPerSec <= 0 {
		return m.StopAxis(a)
	}
	if err := m.SetAxisRate(degPerSec); err != nil {
		return err
	}
	if err := m.Move(axisDirection(a, positive)); err != nil {
		return err
	}
	m.setMoving(true) // the mount will not report this; Slewing ORs it in
	return nil
}

// degPerSecForPreset gives each LX200 preset the rate the vendor's ASCOM driver pairs with it.
func degPerSecForPreset(r lx200.Rate) float64 {
	switch r {
	case lx200.RateMax:
		return AxisRateMax
	case lx200.RateFind:
		return AxisRateMedium
	case lx200.RateCenter:
		return AxisRateSlow
	default: // RateGuide
		return AxisRateSlow / 2
	}
}

// axisDirection maps an axis and sign onto the LX200 direction letter.
func axisDirection(a lx200.Axis, positive bool) lx200.Direction {
	if a == lx200.AxisPrimary {
		if positive {
			return lx200.East
		}
		return lx200.West
	}
	if positive {
		return lx200.North
	}
	return lx200.South
}

// SetSlewSpeed sets one of the three slew-speed slots, 1 to 3, in multiples of sidereal
// (:Cu1= to :Cu3=). Blind; read it back with SlewSpeed.
//
// Slot 3 is what :RS# selects and what a goto runs at, so raising it is how a full-speed manual
// slew is made faster.
func (m *Mount) SetSlewSpeed(n, xSidereal int) error {
	if n < 1 || n > 3 {
		return fmt.Errorf("rainbow: slew-speed preset %d out of range 1..3", n)
	}
	if xSidereal < 0 || xSidereal > 9999 {
		return fmt.Errorf("rainbow: slew speed %d out of range 0..9999 (the field is 4 digits)", xSidereal)
	}
	return m.Blind(fmt.Sprintf(":Cu%d=%04d#", n, xSidereal))
}

// SlewSpeed reads one of the three speed slots, 1 to 3, in multiples of sidereal.
//
// A slot is not the rate the mount is using; it is a value a preset selects. A mount left with
// a low value in slot 3 slews and gotos slowly with nothing reporting why.
func (m *Mount) SlewSpeed(n int) (int, error) {
	if n < 1 || n > 3 {
		return 0, fmt.Errorf("rainbow: slew-speed index %d out of range 1..3", n)
	}
	s, err := m.get(fmt.Sprintf(":CU%d#", n), fmt.Sprintf(":CU%d=", n))
	if err != nil {
		return 0, err
	}
	var v int
	_, err = fmt.Sscanf(s, "%d", &v)
	return v, err
}

// SetForcePierFlip toggles the forced meridian flip (:Af#). When on, a goto to a target that
// has not yet crossed the meridian flips first. Blind.
func (m *Mount) SetForcePierFlip(on bool) error {
	if on {
		return m.Blind(":Af1#")
	}
	return m.Blind(":Af0#")
}

// hms and dmsParts round to wire precision before splitting fields, preserving
// carries at minute and second boundaries.
func hms(hours float64) (h, m int, s float64) {
	t := math.Round(hours*36000) / 10 // seconds, to 0.1
	if t >= 86400 {                   // 23:59:59.97 rounds up to a full day; wrap to 00:00:00.0
		t -= 86400
	}
	h = int(t) / 3600
	t -= float64(h * 3600)
	m = int(t) / 60
	s = t - float64(m*60)
	return
}

func dmsParts(deg float64) (sign byte, d, m int, s float64) {
	sign = '+'
	if deg < 0 {
		sign, deg = '-', -deg
	}
	t := math.Round(deg*36000) / 10 // arcseconds, to 0.1
	d = int(t) / 3600
	t -= float64(d * 3600)
	m = int(t) / 60
	s = t - float64(m*60)
	return
}

// Interfaces this package satisfies.
var (
	_ lx200.Mount           = (*Mount)(nil)
	_ lx200.Homer           = (*Mount)(nil)
	_ lx200.Parker          = (*Mount)(nil)
	_ lx200.PierSider       = (*Mount)(nil) // derived from :CG3#/:CY#
	_ lx200.Horizontal      = (*Mount)(nil)
	_ lx200.Guider          = (*Mount)(nil) // PulseGuide overridden (RST has no :Mg)
	_ lx200.TrackRater      = (*Mount)(nil) // overridden (:Ct? not :T?)
	_ lx200.SiteSetter      = (*Mount)(nil)
	_ lx200.Clock           = (*Mount)(nil) // :SL, via the mount's own :GG# offset
	_ lx200.UTCOffsetSetter = (*Mount)(nil)
	_ lx200.AxisMover       = (*Mount)(nil) // MoveAxis inherited, StopAxis overridden
)
