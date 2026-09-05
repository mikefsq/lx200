package rst

import (
	"errors"
	"testing"

	"github.com/mikefsq/lx200"
)

// Each test here pins one guarantee the Alpaca members add on top of the plain RST operations.

// AlpacaAtHome matches AtHome when the mount is stopped. A session-scoped latch was tried
// instead and dropped, because it reports false for a mount plainly at home after a reconnect.
func TestAlpacaAtHomeEqualsAtHome(t *testing.T) {
	m, _ := newMount(map[string]string{":CY#": ":CY+000.00/-000.00#"})
	alp, err1 := m.AlpacaAtHome()
	raw, err2 := m.AtHome()
	if err1 != nil || err2 != nil || alp != raw || !alp {
		t.Errorf("AlpacaAtHome=%v AtHome=%v (%v,%v); want both true and equal", alp, raw, err1, err2)
	}

	// Not homed, so both false.
	m.mu.Lock()
	m.homeFound = false
	m.mu.Unlock()
	if alp, _ := m.AlpacaAtHome(); alp {
		t.Error("AlpacaAtHome = true without HomeFound")
	}
}

// Arriving is not parked. parkedAxes is a real :CY# reading from a completed park.
const parkedAxes = ":CY+089.98/-000.03#"

// awayAxes is a reading well clear of both the park and home signatures, for tests that need
// Park to take its slewing path rather than the already-there one.
const awayAxes = ":CY+045.00/-010.00#"

func TestAlpacaAtParkIsFalseWhileSlewing(t *testing.T) {
	m, _ := newMount(map[string]string{":CY#": parkedAxes})

	if at, err := m.AlpacaAtPark(); err != nil || !at {
		t.Fatalf("AlpacaAtPark = %v, %v; want true", at, err)
	}
	m.setSlewing(true)
	if at, err := m.AlpacaAtPark(); err != nil || at {
		t.Errorf("AlpacaAtPark = %v, %v; want false while slewing", at, err)
	}
}

// Unpark does not move the mount, so AtPark still reads the park position. A client asking
// whether it is parked means the state, not the place.
func TestAlpacaAtParkHonoursUnpark(t *testing.T) {
	m, _ := newMount(map[string]string{":CY#": parkedAxes})
	if at, err := m.AlpacaAtPark(); err != nil || !at {
		t.Fatalf("AlpacaAtPark = %v, %v; want true on the polar axis", at, err)
	}
	if err := m.AlpacaUnpark(); err != nil {
		t.Fatalf("AlpacaUnpark: %v", err)
	}
	if at, err := m.AlpacaAtPark(); err != nil || at {
		t.Errorf("AlpacaAtPark = %v, %v; want false after Unpark", at, err)
	}
	// The un-guarded member still reads the axes; that is the divergence.
	if at, _ := m.AtPark(); !at {
		t.Error("AtPark should still report the axes; the Alpaca guard is what honours Unpark")
	}
}

// ASCOM requires motion to be refused while parked, rather than silently doing nothing.
func TestAlpacaFindHomeRefusedWhileParked(t *testing.T) {
	m, f := newMount(map[string]string{":CY#": parkedAxes})

	err := m.AlpacaFindHome()
	if !errors.Is(err, ErrParked) {
		t.Errorf("AlpacaFindHome while parked = %v; want ErrParked", err)
	}
	for _, w := range f.Writes() {
		if w == ":Ch#" || w == ":CtL#" {
			t.Errorf("AlpacaFindHome wrote %q while parked; no motion should reach the mount", w)
		}
	}
}

// Parking an already parked mount must not re-run the goto: the underlying Park always slews.
func TestAlpacaParkIsIdempotent(t *testing.T) {
	m, f := newMount(map[string]string{":CY#": parkedAxes})

	if err := m.AlpacaPark(); err != nil {
		t.Fatalf("AlpacaPark: %v", err)
	}
	for _, w := range f.Writes() {
		if w == ":MS#" || w == ":GS#" {
			t.Errorf("AlpacaPark wrote %q when already parked; want no motion", w)
		}
	}
}

// Unparking an unparked mount is a no-op, not an error.
func TestAlpacaUnparkIsIdempotent(t *testing.T) {
	m, _ := newMount(map[string]string{":CY#": ":CY+045.00/-010.00#"}) // not on the polar axis
	if err := m.AlpacaUnpark(); err != nil {
		t.Fatalf("AlpacaUnpark on an unparked mount: %v", err)
	}
}

// Unpark is a state change and does not start tracking
func TestUnparkSendsNothingAndLeavesTrackingAlone(t *testing.T) {
	m, f := newMount(map[string]string{":CY#": parkedAxes})
	if err := m.Unpark(); err != nil {
		t.Fatalf("Unpark: %v", err)
	}
	if w := f.Writes(); len(w) != 0 {
		t.Errorf("Unpark wrote %v; it must send nothing", w)
	}
	// The state changed even though nothing went out on the wire.
	if at, err := m.AlpacaAtPark(); err != nil || at {
		t.Errorf("AlpacaAtPark = %v, %v after Unpark; want false", at, err)
	}
	// ... and the axes still report the park position, which is the deliberate divergence.
	if at, err := m.AtPark(); err != nil || !at {
		t.Errorf("AtPark = %v, %v after Unpark; the tube has not moved", at, err)
	}
}

// A stow at the pole in the wrong rotation must not read as parked. Both readings are from
// hardware, and both point at the celestial pole, so only the RA axis distinguishes them.
func TestAtParkRejectsAPoleStowInTheWrongRotation(t *testing.T) {
	for _, c := range []struct{ name, cy string }{
		{"alt/az goto to the pole — tube out to the side", ":CY+090.00/-080.62#"},
		{"Dec target rounded to 90 — RA never pinned", ":CY+090.00/-030.09#"},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, _ := newMount(map[string]string{":CY#": c.cy})
			if at, err := m.AtPark(); err != nil || at {
				t.Errorf("AtPark = %v, %v at %s; want false — the RA axis is not at 0", at, err, c.cy)
			}
		})
	}
}

// The handset park at +089.49/-000.00 is outside the driver's Dec tolerance.
// Hardware measurements are needed before widening that tolerance.
func TestAtParkDoesNotYetRecogniseTheHandsetPreset(t *testing.T) {
	m, _ := newMount(map[string]string{":CY#": ":CY+089.49/-000.00#"})
	if at, _ := m.AtPark(); at {
		t.Skip("handset preset now recognised — re-measure and update parkDecAxisMin, then delete this test")
	}
}

// SetPark must report itself unimplemented; both of this mount's positions are mechanical.
func TestAlpacaSetParkIsNotImplemented(t *testing.T) {
	m, f := newMount(nil)
	if m.AlpacaCanSetPark() {
		t.Error("AlpacaCanSetPark = true; home is the West horizon and park is the RA axis, both fixed")
	}
	if err := m.AlpacaSetPark(); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("AlpacaSetPark = %v; want ErrNotImplemented", err)
	}
	if f.LastWrite() != "" {
		t.Errorf("AlpacaSetPark wrote %q; it must send nothing", f.LastWrite())
	}
}

// Park reaches the polar-axis stow by hour angle, as an equatorial goto. See
// TestParkCommandsHourAnglePlusSixNotThePolePosition for why the alt/az route is not
// equivalent.
func TestParkGoesToThePolarAxis(t *testing.T) {
	m, f := newMount(map[string]string{
		":CtL#":           ":CTL#",
		":GS#":            ":GS16:00:00#",
		":Sr10:00:00.0#":  "1",
		":Sd+89*59'00.0#": "1",
	})
	if err := m.Park(); err != nil {
		t.Fatalf("Park: %v", err)
	}
	var sawGoto bool
	for _, w := range f.Writes() {
		if w == ":MS#" {
			sawGoto = true
		}
	}
	if !sawGoto {
		t.Errorf("Park wrote %q; want the equatorial goto :MS#", f.Writes())
	}
}

// The mount can home, park and unpark, but neither of its positions can be redefined.
func TestAlpacaCapabilities(t *testing.T) {
	m, _ := newMount(nil)
	if !m.AlpacaCanFindHome() || !m.AlpacaCanPark() || !m.AlpacaCanUnpark() {
		t.Error("the RST supports FindHome, Park and Unpark")
	}
	if m.AlpacaCanSetPark() {
		t.Error("both of this mount's positions are mechanical; there is no park to set")
	}
}

// ASCOM requires Slewing to be true during a MoveAxis. The mount does not report it: a manual
// move sets no completion-token latch, confirmed on hardware. The driver keeps its own flag.
func TestSlewingCoversContinuousMoves(t *testing.T) {
	m, _ := newMount(nil)
	if sl, _ := m.Slewing(); sl {
		t.Fatal("Slewing should start false")
	}
	if err := m.MoveAxisRate(lx200.AxisPrimary, true, AxisRateMedium); err != nil {
		t.Fatalf("MoveAxisRate: %v", err)
	}
	if sl, err := m.Slewing(); err != nil || !sl {
		t.Errorf("Slewing = %v, %v during a MoveAxis; ASCOM requires true", sl, err)
	}
	if err := m.StopAxis(lx200.AxisPrimary); err != nil {
		t.Fatalf("StopAxis: %v", err)
	}
	if sl, err := m.Slewing(); err != nil || sl {
		t.Errorf("Slewing = %v, %v after StopAxis; want false", sl, err)
	}
}

// Halt ends a continuous move as well as a goto.
func TestHaltClearsAContinuousMove(t *testing.T) {
	m, _ := newMount(nil)
	if err := m.MoveAxisRate(lx200.AxisSecondary, false, AxisRateMedium); err != nil {
		t.Fatal(err)
	}
	if sl, _ := m.Slewing(); !sl {
		t.Fatal("Slewing should be true while moving")
	}
	if err := m.Halt(); err != nil {
		t.Fatalf("Halt: %v", err)
	}
	if sl, _ := m.Slewing(); sl {
		t.Error("Slewing still true after Halt")
	}
}

// ASCOM requires Slewing to be false during a guide pulse. PulseGuide uses the same move
// commands, so it must not set the move flag.
func TestPulseGuideDoesNotSetSlewing(t *testing.T) {
	m, _ := newMount(map[string]string{":CtU#": ":CTU#"})
	if err := m.PulseGuide(lx200.East, 50); err != nil {
		t.Fatalf("PulseGuide: %v", err)
	}
	if sl, err := m.Slewing(); err != nil || sl {
		t.Errorf("Slewing = %v, %v during a pulse guide; ASCOM requires false", sl, err)
	}
	if !m.IsPulseGuiding() {
		t.Error("IsPulseGuiding should be true instead")
	}
}

// AxisAngles parses the Dec and RA mechanical angles from :CY#. The fixtures are real hardware
// readings, and the equatorial coordinates cannot distinguish them: both are Dec about 90,
// but the axis angles can, which is why the accessor exists.
func TestAxisAngles(t *testing.T) {
	cases := []struct {
		reply        string
		wantDec, wRA float64
	}{
		{":CY+089.49/-000.00#", 89.49, 0.00},   // handset polar-axis park (top up)
		{":CY+090.00/-080.62#", 90.00, -80.62}, // our alt/az park (saddle left)
		{":CY+000.00/-001.10#", 0.00, -1.10},   // idle, near home
	}
	for _, c := range cases {
		m, _ := newMount(map[string]string{":CY#": c.reply})
		dec, ra, err := m.AxisAngles()
		if err != nil || !near(dec, c.wantDec) || !near(ra, c.wRA) {
			t.Errorf("AxisAngles(%s) = %v, %v, %v; want %v, %v", c.reply, dec, ra, err, c.wantDec, c.wRA)
		}
	}
}

// Re-homing a mount that is ALREADY at home: the axes read 0/0 for the whole seek, so the
// position test says "at home" from the first instant. A Platform 7 client polls AtHome to learn
// when FindHome finished, and would conclude it finished before the mount moved. This holds
// however the mount derives :CY#, since the axes really are at zero throughout, so the guard is
// needed on the ASCOM contract alone.
func TestAlpacaAtHomeIsFalseWhileHoming(t *testing.T) {
	m, _ := newMount(map[string]string{":CY#": ":CY+000.00/-000.00#"})
	if at, err := m.AlpacaAtHome(); err != nil || !at {
		t.Fatalf("AlpacaAtHome = %v, %v; want true when stopped at home", at, err)
	}
	m.setSlewing(true) // a home seek is now running
	if at, err := m.AlpacaAtHome(); err != nil || at {
		t.Errorf("AlpacaAtHome = %v, %v; want false while a seek is running — the axes still "+
			"read the pre-seek position and the encoder zero is being redefined", at, err)
	}
	// The un-guarded member still answers from the axes; the Alpaca guard is what adds "stopped".
	if at, _ := m.AtHome(); !at {
		t.Error("AtHome should still report the axes; AlpacaAtHome is what adds the stopped requirement")
	}
}

// :CY# is relative to the reference the last home established, so before any home this
// power-cycle it has no defined origin. Both position members must refuse to answer from it.
func TestAtHomeAndAtParkRequireAHomeReference(t *testing.T) {
	for _, c := range []struct{ name, cy string }{
		{"axes reading as at home", ":CY+000.00/-000.00#"},
		{"axes reading as parked", parkedAxes},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, _ := newMount(map[string]string{":CY#": c.cy})
			m.mu.Lock()
			m.homeFound = false
			m.mu.Unlock()
			if at, _ := m.AtHome(); at {
				t.Error("AtHome = true without a home reference")
			}
			if at, _ := m.AtPark(); at {
				t.Error("AtPark = true without a home reference — :CY# has no origin until a home zeroes the encoders")
			}
		})
	}
}
