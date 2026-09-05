// Package onstep drives OnStep and OnStepX telescope controllers over serial and TCP.
package onstep

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mikefsq/lx200"
	"github.com/mikefsq/lx200/serial"
)

const statusTTL = 150 * time.Millisecond

// Mount is an OnStep controller on a LX200 connection.
type Mount struct {
	*lx200.Conn

	mu       sync.Mutex
	cached   Status
	cachedAt time.Time
}

// Open connects over serial (OnStep's default 9600 baud).
func Open(portName string) (*Mount, error) {
	tr, err := serial.Open(portName, 9600)
	if err != nil {
		return nil, err
	}
	return newMount(tr), nil
}

// Dial connects over TCP (OnStep WiFi/Ethernet, typically :9999 or :9996).
func Dial(addr string) (*Mount, error) {
	tr, err := lx200.DialTCP(addr, 5*time.Second)
	if err != nil {
		return nil, err
	}
	return newMount(tr), nil
}

func newMount(tr lx200.Transport) *Mount {
	m := &Mount{Conn: lx200.New(tr, 3*time.Second)}
	// OnStepX emits startup junk; a couple of :GVP# pokes flush it (best-effort).
	_, _ = m.Get(":GVP#")
	_, _ = m.Get(":GVP#")
	return m
}

// Product returns the controller product name (:GVP#).
func (m *Mount) Product() (string, error) { return m.Get(":GVP#") }

// Status is the decoded :GU# status. OnStep's encoding (per the INDI driver):
// 'n'+'N' = idle, 'n' alone = slewing, 'N' alone = tracking; 'I' = parking,
// 'P' = parked, 'H' = at home, 'A' = AltAz mount.
type Status struct {
	Slewing  bool
	Tracking bool
	Parked   bool
	AtHome   bool
	AltAz    bool
}

func parseGU(s string) Status {
	has := func(c byte) bool { return strings.IndexByte(s, c) >= 0 }
	n, N := has('n'), has('N') // 'n' = not tracking, 'N' = not goto (negative flags)
	parked := has('P')
	st := Status{
		Parked: parked,
		AtHome: has('H'),
		AltAz:  has('A'),
	}
	if !parked { // INDI gives 'P' (parked) priority over the motion flags
		st.Slewing = (n && !N) || has('I') // goto in progress (or parking)
		st.Tracking = N && !n
	}
	return st
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
	m.cached, m.cachedAt = parseGU(gu), time.Now()
	return m.cached, nil
}

func (m *Mount) invalidate() {
	m.mu.Lock()
	m.cachedAt = time.Time{}
	m.mu.Unlock()
}

func (m *Mount) Slewing() (bool, error)  { s, err := m.status(); return s.Slewing, err }
func (m *Mount) Tracking() (bool, error) { s, err := m.status(); return s.Tracking, err }

func (m *Mount) SetTracking(on bool) error {
	cmd := ":Td#"
	if on {
		cmd = ":Te#"
	}
	if err := m.okByte(cmd, '0'); err != nil { // OnStep acks '0' = success
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

// Parker — native park/unpark (:hP#/:hR#); AtPark from :GU#.
func (m *Mount) Park() error {
	if err := m.Blind(":hP#"); err != nil {
		return err
	}
	m.invalidate()
	return nil
}

func (m *Mount) Unpark() error {
	if err := m.okByte(":hR#", '1'); err != nil { // getCommandSingleCharResponse: '1' = success
		return err
	}
	m.invalidate()
	return nil
}

func (m *Mount) AtPark() (bool, error) { s, err := m.status(); return s.Parked, err }

// Homer — go home (:hC#); AtHome from :GU#. (Set-home is SetHome below.)
func (m *Mount) FindHome() error {
	if err := m.Blind(":hC#"); err != nil {
		return err
	}
	m.invalidate()
	return nil
}

func (m *Mount) AtHome() (bool, error) { s, err := m.status(); return s.AtHome, err }

// PierSider — via :Gm# ('W'/'E'); undefined in AltAz.
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

// SiteSetter — OnStep uses ':'-separated sexagesimal; longitude East-negative.
func (m *Mount) SetSiteLatitude(deg float64) error {
	return ack(m.Ack(":St" + dms(deg, 2) + "#"))
}

func (m *Mount) SetSiteLongitude(deg float64) error {
	return ack(m.Ack(":Sg" + dms(-deg, 3) + "#"))
}

func (m *Mount) SetSiteElevation(meters float64) error {
	return ack(m.Ack(fmt.Sprintf(":Sev%+07.1f#", meters)))
}

// Clock — set UTC offset 0 + UTC date/time (mount treats UTC as local).
func (m *Mount) SetUTC(t time.Time) error {
	u := t.UTC()
	if err := ack(m.Ack(":SG+00:00#")); err != nil {
		return err
	}
	if err := ack(m.Ack(u.Format(":SC01/02/06#"))); err != nil {
		return err
	}
	return ack(m.Ack(u.Format(":SL15:04:05#")))
}

func (m *Mount) SetRate(r lx200.Rate) error { return m.SetSlewRateIndex(rateIndex(r)) }

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
		return 2
	case lx200.RateCenter:
		return 5
	case lx200.RateFind:
		return 7
	default: // RateMax
		return 9
	}
}

// MoveAxis is overridden so it uses OnStep's numbered SetRate (the core's
// MoveAxis would call the core's letter-preset SetRate statically).
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

// MountType reports the configured mount geometry (:GXEM#): 'E' GEM, 'K' Fork,
// 'k' Fork-Alt, 'A' AltAz.
func (m *Mount) MountType() (string, error) { return m.Get(":GXEM#") }

// SetHome stores the current position as the home/reset position (:hF#).
func (m *Mount) SetHome() error { return m.Blind(":hF#") }

// SetParkHere stores the current position as the park position (:hQ#).
func (m *Mount) SetParkHere() error { return m.okByte(":hQ#", '1') }

// SetCustomTrackRate sets the RA and Dec tracking rates in arcsec/sec
// (:RA####/:RE####), for non-sidereal tracking.
func (m *Mount) SetCustomTrackRate(raRate, decRate float64) error {
	if err := m.okByte(fmt.Sprintf(":RA%04.4f#", raRate), '0'); err != nil {
		return err
	}
	return m.okByte(fmt.Sprintf(":RE%04.4f#", decRate), '0')
}

func ack(ok bool, err error) error {
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("onstep: mount rejected command")
	}
	return nil
}

// okByte sends an OnStep "set" command that replies a single status byte (no
// '#') and verifies it, consuming the byte so it can't desync the next :GU#
// poll. OnStep's success value differs by command family: '0' for the
// sendOnStepCommand-style acks (:Te/:Td/:RA/:RE), '1' for the
// getCommandSingleCharResponse-style ones (:hQ/:hR).
func (m *Mount) okByte(cmd string, success byte) error {
	b, err := m.AckByte(cmd)
	if err != nil {
		return err
	}
	if b != success {
		return fmt.Errorf("onstep: %s rejected (reply %q)", cmd, string(b))
	}
	return nil
}

// dms formats signed degrees as "sDDD:MM:SS" (OnStep uses ':' throughout) at a
// fixed degree-field width (2 latitude, 3 longitude).
func dms(deg float64, degWidth int) string {
	sign := byte('+')
	if deg < 0 {
		sign, deg = '-', -deg
	}
	t := int(deg*3600 + 0.5)
	return fmt.Sprintf("%c%0*d:%02d:%02d", sign, degWidth, t/3600, (t/60)%60, t%60)
}

var (
	_ lx200.Mount      = (*Mount)(nil)
	_ lx200.Parker     = (*Mount)(nil)
	_ lx200.Homer      = (*Mount)(nil)
	_ lx200.PierSider  = (*Mount)(nil)
	_ lx200.SiteSetter = (*Mount)(nil)
	_ lx200.Clock      = (*Mount)(nil)
	_ lx200.Guider     = (*Mount)(nil) // inherited (:Mg pulse-guide)
	_ lx200.AxisMover  = (*Mount)(nil) // MoveAxis overridden
	_ lx200.TrackRater = (*Mount)(nil) // inherited (:TQ/:TL/:TS)
	_ lx200.Horizontal = (*Mount)(nil) // inherited (:GA#/:GZ#)
)
