// Package am5 drives ZWO AM-series harmonic mounts (AM3/AM5/AM5N/AM7) over the
// LX200 protocol, on the shared golx200 core. ZWO mounts speak USB-serial (9600)
// and WiFi/TCP (default 192.168.4.1:4030); both are supported.
//
// It follows the gotenmicron reference pattern (embed *lx200.Conn, a cached
// status, the lx200.Mount + optional capabilities), with two AM5-specific
// twists: status is split across :GU# (slew/home/mode) and :GAT# (tracking),
// and slew rates are numbered (:R0#–:R9#) rather than the core's letter presets,
// so SetRate AND MoveAxis are overridden (the core's MoveAxis calls the core's
// SetRate statically — Go embedding is not virtual).
package am5

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mikefsq/lx200"
	"github.com/mikefsq/lx200/serial"
)

const statusTTL = 150 * time.Millisecond

// Mount is a ZWO AM-series mount on a golx200 LX200 connection.
type Mount struct {
	*lx200.Conn

	mu       sync.Mutex
	cached   Status
	cachedAt time.Time
	parked   bool // AM5 has no native park state; Park == go-home + this flag
}

// Open connects over USB-serial at 9600 baud (the ZWO documented rate).
func Open(portName string) (*Mount, error) {
	tr, err := serial.Open(portName, 9600)
	if err != nil {
		return nil, err
	}
	return &Mount{Conn: lx200.New(tr, 3*time.Second)}, nil
}

// Dial connects over TCP/WiFi, e.g. Dial("192.168.4.1:4030").
func Dial(addr string) (*Mount, error) {
	tr, err := lx200.DialTCP(addr, 5*time.Second)
	if err != nil {
		return nil, err
	}
	return &Mount{Conn: lx200.New(tr, 3*time.Second)}, nil
}

// --- Status (:GU# + :GAT#) --------------------------------------------------

// Status is the decoded AM5 status. :GU# is parsed by character presence (as the
// ZWO firmware/INDI driver do): 'N' = slew complete, 'H' = at home, 'Z' = AltAz
// mode, 'G' = equatorial mode. Tracking is a separate :GAT# query.
type Status struct {
	Slewing  bool
	AtHome   bool
	AltAz    bool
	Tracking bool
}

func (m *Mount) status() (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.cachedAt.IsZero() && time.Since(m.cachedAt) < statusTTL {
		return m.cached, nil
	}
	gu, err := m.Get(":GU#")
	if err != nil {
		return Status{}, err
	}
	st := Status{
		Slewing: !strings.ContainsRune(gu, 'N'), // 'N' present == slew complete
		AtHome:  strings.ContainsRune(gu, 'H'),
		AltAz:   strings.ContainsRune(gu, 'Z'), // 'G' == equatorial
	}
	gat, err := m.Get(":GAT#")
	if err != nil {
		return Status{}, err
	}
	st.Tracking = len(gat) > 0 && gat[0] == '1'

	m.cached, m.cachedAt = st, time.Now()
	return st, nil
}

func (m *Mount) invalidate() {
	m.mu.Lock()
	m.cachedAt = time.Time{}
	m.mu.Unlock()
}

// --- lx200.Mount ------------------------------------------------------------
// RA/Dec/SetTarget*/Halt come from the embedded core (:GR#/:GD#/:Sr/:Sd/:Q#).

func (m *Mount) Slewing() (bool, error)  { s, err := m.status(); return s.Slewing, err }
func (m *Mount) Tracking() (bool, error) { s, err := m.status(); return s.Tracking, err }

// SetTracking enables (:Te#) or disables (:Td#) tracking; each replies '1'/'0'.
func (m *Mount) SetTracking(on bool) error {
	cmd := ":Td#"
	if on {
		cmd = ":Te#"
	}
	if err := must(m.Ack(cmd)); err != nil {
		return err
	}
	m.invalidate()
	return nil
}

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

// --- Optional capabilities --------------------------------------------------

// Parker: AM5 has no distinct park position — Park goes home (:hC#) and we track
// the parked state in software; Unpark just clears it.
func (m *Mount) Park() error {
	if err := m.Blind(":hC#"); err != nil {
		return err
	}
	m.mu.Lock()
	m.parked = true
	m.mu.Unlock()
	m.invalidate()
	return nil
}

func (m *Mount) Unpark() error {
	m.mu.Lock()
	m.parked = false
	m.mu.Unlock()
	m.invalidate()
	return nil
}

func (m *Mount) AtPark() (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.parked, nil
}

// Homer: go-home (:hC#); AtHome comes from :GU#. AM5 also supports storing the
// current position as home (:SOa#), exposed as SetHome below.
func (m *Mount) FindHome() error {
	if err := m.Blind(":hC#"); err != nil {
		return err
	}
	m.invalidate()
	return nil
}

func (m *Mount) AtHome() (bool, error) { s, err := m.status(); return s.AtHome, err }

// SetHome stores the current position as the home position (:SOa#, acks '1').
func (m *Mount) SetHome() error { return must(m.Ack(":SOa#")) }

// PierSider: :Gm# ('W'/'E'); undefined in AltAz mode.
func (m *Mount) PierSide() (lx200.PierSide, error) {
	s, err := m.status()
	if err != nil {
		return lx200.PierUnknown, err
	}
	if s.AltAz {
		return lx200.PierUnknown, nil
	}
	res, err := m.Get(":Gm#")
	if err != nil {
		return lx200.PierUnknown, err
	}
	switch {
	case strings.ContainsRune(res, 'W'):
		return lx200.PierWest, nil
	case strings.ContainsRune(res, 'E'):
		return lx200.PierEast, nil
	}
	return lx200.PierUnknown, nil
}

// SiteSetter — AM5 longitude is Meade-reversed (Alpaca is East-positive).
func (m *Mount) SetSiteLatitude(deg float64) error {
	return must(m.Ack(":St" + dms(deg, 2) + "#"))
}

func (m *Mount) SetSiteLongitude(deg float64) error {
	return must(m.Ack(":Sg" + dms(-deg, 3) + "#"))
}

// SetSiteElevation is a no-op: INDI's AM5 driver never sends elevation (it does
// not affect pointing). Present to satisfy SiteSetter.
func (m *Mount) SetSiteElevation(meters float64) error { return nil }

// Clock — mirror INDI's AM5 driver: set the (negated) UTC offset and the local
// date. AM5's INDI driver sends neither local time (:SL) nor elevation; the
// mount keeps its own time-of-day, so date + offset is all it sets. A UTC-zoned
// t yields a zero offset and a UTC date.
func (m *Mount) SetUTC(t time.Time) error {
	_, offSec := t.Zone() // seconds east of UTC (local = UTC + offSec)
	off := -offSec        // :SG is "hours added to local to yield UTC" (INDI negates)
	sign := byte('+')
	if off < 0 {
		sign, off = '-', -off
	}
	if err := must(m.Ack(fmt.Sprintf(":SG%c%02d:%02d#", sign, off/3600, (off%3600)/60))); err != nil {
		return err
	}
	return must(m.Ack(t.Format(":SC01/02/06#"))) // local date, mm/dd/yy
}

// --- Slew rate (numbered) + MoveAxis override -------------------------------

// SetRate maps the core's four letter presets onto AM5's numbered rates. It
// overrides the embedded core's SetRate so manual slewing uses :R0#–:R9#.
func (m *Mount) SetRate(r lx200.Rate) error { return m.SetSlewRateIndex(rateIndex(r)) }

// SetSlewRateIndex selects one of AM5's 10 slew rates (0=0.25x … 9=1440x), :R%d#.
func (m *Mount) SetSlewRateIndex(i int) error {
	if i < 0 {
		i = 0
	} else if i > 9 {
		i = 9
	}
	return m.Blind(fmt.Sprintf(":R%d#", i))
}

func rateIndex(r lx200.Rate) int {
	switch r {
	case lx200.RateGuide:
		return 0
	case lx200.RateCenter:
		return 4
	case lx200.RateFind:
		return 6
	default: // RateMax
		return 9
	}
}

// MoveAxis must be overridden: the core's MoveAxis calls the core's SetRate
// statically (Go embedding is not virtual), which would emit :RG# instead of
// AM5's :R0#. So compose AM5's SetRate with the core's (universal) Move.
func (m *Mount) MoveAxis(a lx200.Axis, positive bool, r lx200.Rate) error {
	if err := m.SetRate(r); err != nil {
		return err
	}
	return m.Move(axisDir(a, positive))
}

func axisDir(a lx200.Axis, positive bool) lx200.Direction {
	switch {
	case a == lx200.AxisPrimary && positive:
		return lx200.East
	case a == lx200.AxisPrimary:
		return lx200.West
	case positive:
		return lx200.North
	default:
		return lx200.South
	}
}

// --- AM5 vendor commands (exposed by the Alpaca wrapper as Actions) ----------

// MountMode reports the equatorial/AltAz mode (from :GU#).
type MountMode int

const (
	ModeEquatorial MountMode = iota
	ModeAltAz
)

func (m *Mount) MountMode() (MountMode, error) {
	s, err := m.status()
	if err != nil {
		return 0, err
	}
	if s.AltAz {
		return ModeAltAz, nil
	}
	return ModeEquatorial, nil
}

// SetMountMode switches EQ (:AP#) / AltAz (:AA#). Takes effect after a mount
// power-cycle (a ZWO quirk).
func (m *Mount) SetMountMode(mode MountMode) error {
	cmd := ":AP#"
	if mode == ModeAltAz {
		cmd = ":AA#"
	}
	if err := m.Blind(cmd); err != nil {
		return err
	}
	m.invalidate()
	return nil
}

// SetGuideRate sets the guide rate as a fraction of sidereal (0.1–0.9), :Rg.
func (m *Mount) SetGuideRate(x float64) error { return m.Blind(fmt.Sprintf(":Rg%.2f#", x)) }

// GuideRate reads the guide rate (:Ggr#).
func (m *Mount) GuideRate() (float64, error) {
	s, err := m.Get(":Ggr#")
	if err != nil {
		return 0, err
	}
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v, nil
}

// SetVariableSlewRate sets a continuous slew speed in ×sidereal (0–1440), :Rv.
func (m *Mount) SetVariableSlewRate(x float64) error { return m.Blind(fmt.Sprintf(":Rv%.2f#", x)) }

// SetBuzzer sets buzzer volume (0=off,1=low,2=high), :SBu.
func (m *Mount) SetBuzzer(level int) error { return m.Blind(fmt.Sprintf(":SBu%d#", level)) }

// Buzzer reads the buzzer volume (:GBu#): 0 off, 1 low, 2 high.
func (m *Mount) Buzzer() (int, error) {
	s, err := m.Get(":GBu#")
	if err != nil {
		return 0, err
	}
	if s == "" {
		return 0, fmt.Errorf("am5: empty :GBu# reply")
	}
	return int(s[0] - '0'), nil
}

// ClearAlignment clears the multi-star alignment (:NSC#).
func (m *Mount) ClearAlignment() error { return must(m.Ack(":NSC#")) }

// MeridianFlip is AM5's automatic meridian-flip configuration (:GTa# / :STa#).
type MeridianFlip struct {
	Enabled   bool // perform an automatic meridian flip at the limit
	TrackPast bool // keep tracking past the limit (vs stop)
	LimitDeg  int  // flip limit angle past the meridian, degrees (signed)
}

// MeridianFlip reads the auto-flip configuration (:GTa#). Reply layout:
// [0] enabled, [1] track-past, [2] sign, [3:5] limit degrees.
func (m *Mount) MeridianFlip() (MeridianFlip, error) {
	s, err := m.Get(":GTa#")
	if err != nil {
		return MeridianFlip{}, err
	}
	if len(s) < 5 {
		return MeridianFlip{}, fmt.Errorf("am5: short :GTa# reply %q", s)
	}
	limit, _ := strconv.Atoi(s[3:5])
	if s[2] == '-' {
		limit = -limit
	}
	return MeridianFlip{Enabled: s[0] == '1', TrackPast: s[1] == '1', LimitDeg: limit}, nil
}

// SetMeridianFlip sets the auto-flip configuration (:STa%d%d%c%02d#, acks '1').
func (m *Mount) SetMeridianFlip(f MeridianFlip) error {
	sign, limit := byte('+'), f.LimitDeg
	if limit < 0 {
		sign, limit = '-', -limit
	}
	return must(m.Ack(fmt.Sprintf(":STa%d%d%c%02d#", b2i(f.Enabled), b2i(f.TrackPast), sign, limit)))
}

// --- helpers ----------------------------------------------------------------

func must(ok bool, err error) error {
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("am5: mount rejected command")
	}
	return nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// dms formats signed degrees as "sDDD*MM:SS" at a fixed degree-field width
// (2 for latitude, 3 for longitude), rounded to the nearest arcsecond.
func dms(deg float64, degWidth int) string {
	sign := byte('+')
	if deg < 0 {
		sign, deg = '-', -deg
	}
	t := int(deg*3600 + 0.5)
	return fmt.Sprintf("%c%0*d*%02d:%02d", sign, degWidth, t/3600, (t/60)%60, t%60)
}

var (
	_ lx200.Mount      = (*Mount)(nil)
	_ lx200.Parker     = (*Mount)(nil)
	_ lx200.Homer      = (*Mount)(nil)
	_ lx200.PierSider  = (*Mount)(nil)
	_ lx200.SiteSetter = (*Mount)(nil)
	_ lx200.Clock      = (*Mount)(nil)
	_ lx200.Guider     = (*Mount)(nil) // inherited from *Conn
	_ lx200.AxisMover  = (*Mount)(nil) // MoveAxis overridden, StopAxis inherited
	_ lx200.TrackRater = (*Mount)(nil) // inherited from *Conn
	_ lx200.Horizontal = (*Mount)(nil) // inherited from *Conn (:GA#/:GZ#)
)
