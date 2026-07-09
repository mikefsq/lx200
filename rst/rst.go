// Package rst drives Rainbow Astro RST harmonic mounts (RST-135/300) over
// their LX200-derived serial dialect, on the shared golx200 core. RST is the
// outlier of the LX200 family: it has no status query — instead it pushes an
// unsolicited completion token (:MM0# / :CHO#) when a slew or home finishes — and
// its replies echo the command prefix (:GR# -> :GR20:28:56.9#). Both are handled
// here. Protocol details follow the observed RST serial protocol.
package rst

import (
	"fmt"
	"math"
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
	targetRA  float64 // remembered for the degrees-based :Ck sync
	targetDec float64
	pulsing   bool

	homeFound bool         // a home seek has completed since power-on (gates slews)
	home      homePosition // West-horizon home coords, captured on home completion
	siteLat   float64      // cached site latitude (for the pole/park position)
	siteLatOK bool
	parked    bool // at the polar-axis park (latched on Park's completion token)
	parking   bool // a Park slew is in flight; latches parked on the :MM0# token

	homeCapturePending bool // a :CHO arrived; capture the home coords at the next safe read
}

// homeParkTolDeg is the Az/Alt match tolerance for "at home" / "at park" — a mount
// settled on a mechanical position reports its Az/Alt to well within this.
const homeParkTolDeg = 0.5

// homePosition is the mount's mechanical home (West horizon), captured the moment a
// FindHome completes. Az/Alt are effectively fixed (~270/0); RA/Dec are the values
// at the capture instant (they drift with sidereal time thereafter).
type homePosition struct {
	valid            bool
	ra, dec, az, alt float64
}

// Open connects over USB-serial at 115200 baud.
func Open(portName string) (*Mount, error) {
	tr, err := serial.Open(portName, baud)
	if err != nil {
		return nil, err
	}
	m := &Mount{Conn: lx200.New(tr, 3*time.Second)}
	m.init()
	return m, nil
}

// init forces the mount's comm mode and protocol, as the vendor HUBO-i driver
// does on every connect (its Readme notes firmware ≥ V.200605 requires it). Both
// are blind (no reply) and idempotent. Best-effort: a write error here surfaces on
// the first real command, so we don't fail the open on it.
func (m *Mount) init() {
	_ = m.Blind(":AU#") // select USB comm mode (:AW# is WiFi)
	_ = m.Blind(":AR#") // select the Rainbow LX200 dialect
	m.drainStale()
	// Seed the home-found latch from the mount's own :GH# (which stays set across a
	// driver reconnect), so we honor a home done via the handheld or a prior session
	// and don't force a redundant re-home.
	if h, err := m.homeLatched(); err == nil && h {
		m.mu.Lock()
		m.homeFound = true
		m.mu.Unlock()
	}
	// Cache the site latitude — used to locate the celestial pole for the polar-axis
	// park and AtPark, so those don't re-read :Gt# on every poll.
	if lat, err := m.SiteLatitude(); err == nil {
		m.mu.Lock()
		m.siteLat, m.siteLatOK = lat, true
		m.mu.Unlock()
	}
}

// atPosition reports whether the mount is currently pointing within homeParkTolDeg
// of (az, alt). Compares Az/Alt only — the fixed mechanical indicators of a
// mount position; RA (and Dec, at the horizon) drift for a fixed ground direction,
// so they cannot identify a home/park spot.
func (m *Mount) atPosition(az, alt float64) (bool, error) {
	curAz, err := m.Azimuth()
	if err != nil {
		return false, err
	}
	curAlt, err := m.Altitude()
	if err != nil {
		return false, err
	}
	dAz := math.Abs(curAz - az)
	if dAz > 180 {
		dAz = 360 - dAz
	}
	return dAz <= homeParkTolDeg && math.Abs(curAlt-alt) <= homeParkTolDeg, nil
}

// polePosition returns the celestial-pole Az/Alt (the polar-axis park target) from
// the cached site latitude: Az 0°/Alt=lat in the north, Az 180°/Alt=|lat| in the
// south.
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

// homeLatched reads the mount's :GH# home-established latch ("O" once homed since
// power-on, "0" before). Distinct from being *currently* at home — it stays "O"
// after slewing away.
func (m *Mount) homeLatched() (bool, error) {
	s, err := m.get(":GH#", ":GH")
	if err != nil {
		return false, err
	}
	return strings.HasPrefix(s, "O"), nil
}

// HomeFound reports whether a home seek has completed since power-on. The RST needs
// a mechanical home before it will slew reliably, so slews are gated on this. It is
// seeded from :GH# on connect and set when FindHome completes.
func (m *Mount) HomeFound() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.homeFound }

// HomePosition returns the West-horizon home coordinates captured at the last
// successful home (ok=false if none captured this session).
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

// drainStale consumes any unsolicited completion tokens (:CHO#, :MM0#, :MME#, …)
// the mount queued from a slew or home that finished after a previous session
// disconnected. Left unread, the first would be returned as the reply to this
// session's first command and desync the synchronous protocol. Each Await reads one
// #-terminated token; the first that times out means the buffer is clear.
func (m *Mount) drainStale() {
	for i := 0; i < 8; i++ {
		if _, err := m.Await(150 * time.Millisecond); err != nil {
			return
		}
	}
}

// Find auto-detects the RST by its FTDI USB id (VID 0403 / PID 6001) and opens
// the first match. On macOS the cgo-free port enumerator reports no VID/PID (see
// serial.PortInfo), so there Find falls back to the FTDI VCP name convention
// (/dev/cu.usbserial-*) — less selective (any FTDI adapter matches), but it keeps
// auto-detect working without pulling in the enumerator's macOS cgo path.
func Find() (*Mount, error) {
	ports, err := serial.List()
	if err != nil {
		return nil, err
	}
	if name := findPort(ports); name != "" {
		return Open(name)
	}
	return nil, fmt.Errorf("rainbow: no RST mount found (FTDI 0403:6001)")
}

// findPort picks the RST's serial port from an enumerated list: the exact FTDI USB
// id (VID 0403 / PID 6001) when the VID is reported, else — only when no VID is
// available (macOS) — the FTDI VCP name convention. The VID-empty guard keeps the
// name fallback from claiming an unrelated FTDI adapter on platforms that do report
// VIDs. Returns "" if nothing matches. Split out from Find so it is unit-testable
// without enumerating or opening a real port.
func findPort(ports []serial.PortInfo) string {
	for _, p := range ports {
		if p.IsUSB && p.VID == "0403" && p.PID == "6001" {
			return p.Name
		}
	}
	for _, p := range ports {
		if p.VID == "" && p.IsUSB && strings.Contains(p.Name, "usbserial") {
			return p.Name
		}
	}
	return ""
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
	m.applyToken(tok)
}

// applyToken routes an unsolicited completion/fault token (:MM…#/:CH…#) to slew /
// home / park state. On a home completion it arms homeCapturePending rather than
// capturing inline — whichever path caught the token (drainToken or the get()
// resync) may be mid-read, so the capture is deferred to the next safe read (see
// maybeCaptureHome). Tolerant of the token with or without its leading ':'.
func (m *Mount) applyToken(tok string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch strings.TrimPrefix(tok, ":") {
	case "MM0": // slew complete
		m.slewing = false
		if m.parking { // a Park slew arrived at the pole — latch AtPark
			m.parked = true
			m.parking = false
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

// maybeCaptureHome captures the home position if a :CHO completion armed it — run at
// the top of get(), a safe top-level context (not nested inside a reply read). It
// clears the flag before capturing so the capture's own reads see nothing pending
// and don't recurse. The mount stays at home between the :CHO and this call (no slew
// in between), so the reads record the true home coordinates.
func (m *Mount) maybeCaptureHome() {
	m.mu.Lock()
	pending := m.homeCapturePending
	m.homeCapturePending = false
	m.mu.Unlock()
	if pending {
		m.captureHome()
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
		m.parked = false             // any new slew un-parks
		m.parking = false            // ... and cancels a pending park (Park re-arms it)
		m.homeCapturePending = false // ... and a pending home capture (no longer at home)
	}
	m.mu.Unlock()
}

// Fault returns the last motion-abort token, or "" if the last slew/home completed
// cleanly. The mount enforces its movement limits and reports a violation on the
// completion token: ":MML" = lower-limit, ":MMU" = upper-limit, ":MME" = error,
// ":CH0"/":CH<" = home fail. It is cleared when the next slew or home starts, so a
// non-empty Fault once Slewing() goes false means the move aborted (e.g. hit a
// limit) instead of reaching the target.
func (m *Mount) Fault() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastFault
}

// --- Coordinate reads: strip the echoed prefix so the sign leads ------------

// get sends a query and strips the echoed command prefix from the reply
// (:GR20:28:56.9# -> "20:28:56.9").
//
// It also resyncs past a stray unsolicited completion token: when a slew or home
// finishes, the mount pushes :MM0#/:CHO# asynchronously, and if that lands in the
// buffer just before our reply — a window drainToken's peek can miss — a plain read
// would return the token ("CHO") and a coordinate parse would blow up. So if a reply
// does not carry our command's echoed prefix, we consume it (applying it to slew /
// home / park state) and read on for the real reply.
//
// The write and the skip-past-token resync run as one atomic step (GetMatching holds
// the command lock across the whole thing). That matters because this mount is shared
// by two front-ends — the Alpaca wrapper and the LX200 bridge — that poll it
// concurrently: a resync split across two lock acquisitions would let the other
// front-end's command slip in between and steal our real reply, surfacing as a
// spurious read timeout that tears the connection down.
func (m *Mount) get(cmd, prefix string) (string, error) {
	m.maybeCaptureHome() // capture a home deferred by an earlier :CHO, before this read
	s, err := m.GetMatching(cmd,
		func(r string) bool { return strings.HasPrefix(r, prefix) },
		m.applyToken, // route a stray :MM…/:CH… token; ignore anything else
		3)
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

// RA reads the current right ascension in hours (:GR#).
func (m *Mount) RA() (float64, error) { return m.coord(":GR#", ":GR") }

// Dec reads the current declination in degrees (:GD#).
func (m *Mount) Dec() (float64, error) { return m.coord(":GD#", ":GD") }

// Altitude reads the current altitude above the horizon in degrees (:GA#).
func (m *Mount) Altitude() (float64, error) { return m.coord(":GA#", ":GA") }

// Azimuth reads the current azimuth in degrees, East of North (:GZ#).
func (m *Mount) Azimuth() (float64, error) { return m.coord(":GZ#", ":GZ") }

// --- Target + goto/sync (RST dialect) ---------------------------------------

// SetTargetRA sets the goto/sync target right ascension in hours, remembering it for
// the degrees-based :Ck sync and sending the RST :Sr format. Returns whether the
// mount accepted it.
func (m *Mount) SetTargetRA(hours float64) (bool, error) {
	m.mu.Lock()
	m.targetRA = hours
	m.mu.Unlock()
	h, mm, s := hms(hours)
	return m.Ack(fmt.Sprintf(":Sr%02d:%02d:%04.1f#", h, mm, s))
}

// SetTargetDec sets the goto/sync target declination in degrees, remembering it for
// the degrees-based :Ck sync and sending the RST :Sd format. Returns whether the
// mount accepted it.
func (m *Mount) SetTargetDec(deg float64) (bool, error) {
	m.mu.Lock()
	m.targetDec = deg
	m.mu.Unlock()
	sign, d, mm, s := dmsParts(deg)
	return m.Ack(fmt.Sprintf(":Sd%c%02d*%02d'%04.1f#", sign, d, mm, s))
}

// SlewToTarget starts an equatorial goto (:MS#). RST replies nothing; completion
// arrives later as the :MM0# token, so we mark slewing and let drainToken clear it.
// Gated on HomeFound: the RST needs a mechanical home before a goto is meaningful.
func (m *Mount) SlewToTarget() error {
	if err := m.requireHome("goto"); err != nil {
		return err
	}
	if err := m.Blind(":MS#"); err != nil {
		return err
	}
	m.setSlewing(true)
	return nil
}

// SlewToAltAz slews to a horizontal target: set azimuth (:Sz) and altitude (:Sa),
// then goto (:MA#). Az is degrees East of North (0=N, 90=E, 180=S, 270=W); Alt is
// degrees above the horizon. The RST does not ack :Sz/:Sa, so they go blind;
// completion arrives as the :MM0# token (or an :MML#/:MMU# limit fault — see Fault).
// This is the alt/az goto the vendor driver uses, independent of the mount's
// internal Equat/AltAz mode (which is a hand-controller setting, not on the wire).
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
	if err := m.Blind(":MA#"); err != nil {
		return err
	}
	m.setSlewing(true)
	return nil
}

// SlewToPole points the OTA at the celestial pole for polar alignment: an alt/az
// goto to (Az = 0° true north / 180° in the south, Alt = |site latitude|). Doing it
// in alt/az on purpose — the equatorial pole is Dec 90, an RA singularity a :MS#
// goto handles poorly — this is "send Dec 90 in AltAz". It REQUIRES the mount to be
// homed first: the West-horizon mechanical home is the known reference, and a pole
// slew from an arbitrary position is refused. After it completes, sight the true
// pole through the scope and adjust the mount's physical alt/az to center it.
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

// SyncToTarget re-centers on the remembered target (:Ck). Gated on HomeFound —
// syncing an unhomed mount corrupts its coordinate model.
func (m *Mount) SyncToTarget() (string, error) {
	if err := m.requireHome("sync"); err != nil {
		return "", err
	}
	return "", m.Blind(m.syncCmd(":Ck"))
}

// AddAlignmentPoint syncs to the remembered target and saves it as a star-
// alignment point (:CN) — INDI's "save alignment before sync" behavior, for
// building a multi-point model. Set the target with SetTargetRA/Dec first.
func (m *Mount) AddAlignmentPoint() error {
	if err := m.requireHome("add alignment point"); err != nil {
		return err
	}
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

// Slewing reports whether a goto, home, or park is in progress. It first drains any
// pushed completion token, so the flag reflects a slew that has just finished.
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

// track sends an RST tracking command (:Ct?#) and consumes its echo-ack. Unlike
// the plain LX200 track commands these are NOT blind: the RST echoes the command
// back with the case flipped (:CtA# -> :CTA#, :CtL# -> :CTL#, …), and leaving that
// reply unread desyncs every following query — the next :GR#/:AT# read then returns
// this stale echo instead of its own answer. So we read (and discard) the echo.
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

// TrackSidereal selects the sidereal tracking rate (:CtR#). RST track modes are
// :CtR/:CtS/:CtM, not the core's :TQ/:TS/:TL; note lunar is :CtM#, NOT :CtL# (which
// is tracking-disable — see SetTracking). The map matches the vendor HUBO-i driver.
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

// TrackMode reads the active tracking rate via :Ct?# (-> :CT0#..:CT3#). The map
// 0=Sidereal 1=Solar 2=Lunar 3=custom is confirmed by the vendor HUBO-i driver.
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

// --- Homer (:Ch#, completion via :CHO#; at-home from :GH#) -------------------

// FindHome seeks the mount's mechanical home — the RST's West-horizon reference —
// via :Ch#, stopping tracking first. Completion arrives asynchronously as the pushed
// :CHO# token, which latches HomeFound and captures the home coordinates (see
// AtHome/HomePosition).
func (m *Mount) FindHome() error {
	// Homing collides with active tracking on the RST — with tracking on, :Ch#
	// aborts and pushes a :CH< fail token (confirmed on hardware; the vendor
	// firmware notes homing-collision fixes). Stop tracking first (best-effort:
	// consumes the :CTL# echo), then seek home.
	_ = m.track(":CtL#")
	if err := m.Blind(":Ch#"); err != nil {
		return err
	}
	m.setSlewing(true)
	return nil
}

// AtHome reports whether the mount is CURRENTLY at the West-horizon home — the
// current Az/Alt within tolerance of the coordinates captured at the last FindHome
// (see captureHome). RA is not used: at a fixed ground position it drifts with
// sidereal time. Returns false if no home was captured this power-cycle (use
// HomeFound for "has the mount been homed at all"). Distinct from the :GH# latch.
func (m *Mount) AtHome() (bool, error) {
	_, _, az, alt, ok := m.HomePosition()
	if !ok {
		return false, nil
	}
	return m.atPosition(az, alt)
}

// --- Parker: the RST "park" is the polar-axis stow (OTA along the RA axis) ----

// Park sends the mount to the polar axis — the celestial pole (Az 0/Alt=latitude),
// laying the OTA along the RA axis — with tracking off (a stow position). Always
// slews (a re-park when already there is near-zero motion). AtPark latches true when
// the :MM0# completion token arrives. SlewToPole gates on HomeFound and, via
// setSlewing, clears any prior park.
func (m *Mount) Park() error {
	_ = m.SetTracking(false) // stow with tracking off (like FindHome)
	if err := m.SlewToPole(); err != nil {
		return err
	}
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

// Unpark clears the park latch and re-enables tracking. It does not move the mount
// (it stays physically at the pole until the next slew), but AtPark reads false
// immediately — the ASCOM contract.
func (m *Mount) Unpark() error {
	m.mu.Lock()
	m.parked = false
	m.parking = false
	m.mu.Unlock()
	return m.SetTracking(true)
}

// AtPark reports whether the mount is parked at the polar axis. Latched: set when a
// Park slew completes, cleared by Unpark or any other slew.
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

// StopAxis overrides the core AxisMover: RST has no directional quit
// (:Qn/:Qs/:Qe/:Qw), only the bare :Q# full-stop. MoveAxis stays inherited
// (RST uses the core's letter rates, confirmed by the captures).
func (m *Mount) StopAxis(a lx200.Axis) error { return m.Blind(":Q#") }

// --- SiteSetter (RST uses '*' separators; longitude is East-negative) --------

// SetSiteLatitude sets the observing latitude in degrees (:St#). The RST does not
// ack :St/:Sg (verified on hardware), so it is sent blind, matching the vendor
// driver; read back via SiteLatitude (:Gt#) if needed.
func (m *Mount) SetSiteLatitude(deg float64) error {
	sign, d, mm, s := dmsParts(deg)
	return m.Blind(fmt.Sprintf(":St%c%02d*%02d'%02d#", sign, d, mm, int(s)))
}

// SetSiteLongitude sets the observing longitude in degrees East-positive (:Sg#). The
// RST is East-negative on the wire and does not ack it, so it is sent blind (see
// SetSiteLatitude); read back via SiteLongitude (:Gg#).
func (m *Mount) SetSiteLongitude(deg float64) error {
	sign, d, mm, s := dmsParts(-deg) // negate: RST East = negative
	return m.Blind(fmt.Sprintf(":Sg%c%03d*%02d'%02d#", sign, d, mm, int(s)))
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

// --- Clock / date / site (GPS-fed) ------------------------------------------

// SiderealTime reads local sidereal time (:GS# -> :GS08:40:22), hours. Overrides
// the lx200 core, whose SiderealTime does not strip the RST's echoed :GS prefix.
func (m *Mount) SiderealTime() (float64, error) { return m.coord(":GS#", ":GS") }

// LocalTime reads the mount clock (:GL# -> :GL14:44:27), hours since midnight. The
// GPS keeps it set; there is no serial set (the vendor driver never sets it).
func (m *Mount) LocalTime() (float64, error) { return m.coord(":GL#", ":GL") }

// Date reads the calendar date (:GC# -> :GC07/07/26), returned as "MM/DD/YY" (the
// RST wire format). GPS-fed.
func (m *Mount) Date() (string, error) { return m.get(":GC#", ":GC") }

// UTCOffset reads the timezone offset (:GG# -> :GG+07) in hours, LX200 convention:
// hours to ADD to local time to reach UTC (so +7 = local is UTC-7).
func (m *Mount) UTCOffset() (float64, error) {
	s, err := m.get(":GG#", ":GG")
	if err != nil {
		return 0, err
	}
	var v float64
	_, err = fmt.Sscanf(s, "%f", &v)
	return v, err
}

// SiteLongitude reads the configured longitude (:Gg# -> :Gg+122*30'00.0#) and
// returns it East-positive degrees. RST is East-negative on the wire (matching
// SetSiteLongitude), so the parsed value is negated.
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

// SiteName reads one of the four stored site/park names (1=:GM 2=:GN 3=:GO 4=:GP,
// e.g. "My Home"). These replies carry no echo prefix — just a space-padded name —
// so they are read raw and trimmed.
func (m *Mount) SiteName(n int) (string, error) {
	cmds := map[int]string{1: ":GM#", 2: ":GN#", 3: ":GO#", 4: ":GP#"}
	cmd, ok := cmds[n]
	if !ok {
		return "", fmt.Errorf("rainbow: site name index %d out of range 1..4", n)
	}
	s, err := m.Get(cmd)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(s), nil
}

// --- Controller telemetry ---------------------------------------------------

// SystemStatus is the controller/motor health from :GY# (-> :GYOXOO): four
// 'O'(ok)/'X'(fault) flags; the second field is unused by the vendor driver.
type SystemStatus struct {
	TCS      bool // temperature-control / controller ok
	DecMotor bool
	RAMotor  bool
	Raw      string // the raw flag string, e.g. "OXOO"
}

// SystemStatus reads controller/motor health (:GY#).
func (m *Mount) SystemStatus() (SystemStatus, error) {
	s, err := m.get(":GY#", ":GY")
	if err != nil {
		return SystemStatus{}, err
	}
	ok := func(i int) bool { return i < len(s) && s[i] == 'O' }
	return SystemStatus{TCS: ok(0), DecMotor: ok(2), RAMotor: ok(3), Raw: s}, nil
}

// MotorLoad reads the per-axis motor load percent (:CP# -> :CP+001.9|+004.5),
// returning Dec% and RA%.
func (m *Mount) MotorLoad() (decPct, raPct float64, err error) {
	s, err := m.get(":CP#", ":CP")
	if err != nil {
		return 0, 0, err
	}
	_, err = fmt.Sscanf(s, "%f|%f", &decPct, &raPct)
	return decPct, raPct, err
}

// AutoResume reports whether auto-resume is enabled (:CR# -> :CRR# on / :CRX# off).
func (m *Mount) AutoResume() (bool, error) {
	s, err := m.get(":CR#", ":CR")
	if err != nil {
		return false, err
	}
	return strings.HasPrefix(s, "R"), nil
}

// --- Guide rate / slew speeds / pier-flip config ----------------------------

// GuideRate reads the guide rate as a fraction of sidereal (:CU0# -> :CU0=0.5).
func (m *Mount) GuideRate() (float64, error) {
	s, err := m.get(":CU0#", ":CU0=")
	if err != nil {
		return 0, err
	}
	var v float64
	_, err = fmt.Sscanf(s, "%f", &v)
	return v, err
}

// SetGuideRate sets the guide rate as a fraction of sidereal (:Cu0=<x>#). Blind
// (the RST does not reply), verified on hardware.
func (m *Mount) SetGuideRate(xSidereal float64) error {
	return m.Blind(fmt.Sprintf(":Cu0=%.1f#", xSidereal))
}

// SlewSpeed reads one of the three manual slew-speed presets (n=1..3, :CU1#..:CU3#
// -> e.g. :CU1=0100).
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

// SetForcePierFlip toggles the pre-slew forced meridian flip (:Af1# on / :Af0# off):
// when on, a goto to a target that has not yet crossed the meridian flips first.
// Blind (no reply), verified on hardware.
func (m *Mount) SetForcePierFlip(on bool) error {
	if on {
		return m.Blind(":Af1#")
	}
	return m.Blind(":Af0#")
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
