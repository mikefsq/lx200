package tenmicron

import (
	"testing"
	"time"

	"github.com/mikefsq/lx200"
)

// TestSlewToTargetFirmwareGating: the plain goto sends the vendor default :MSnf# on
// modern (and unknown) firmware, falling back to :MS# only on firmware < 2.11.0;
// SlewToTargetFineLimit always sends :MS#.
func TestSlewToTargetFirmwareGating(t *testing.T) {
	cases := []struct {
		name string
		fw   Version
		call func(*Mount) error
		want string
	}{
		{"modern default", Version{3, 2, 5}, (*Mount).SlewToTarget, ":MSnf#"},
		{"unknown default", Version{}, (*Mount).SlewToTarget, ":MSnf#"},
		{"ancient default", Version{2, 10, 0}, (*Mount).SlewToTarget, ":MS#"},
		{"fine-limit", Version{3, 2, 5}, (*Mount).SlewToTargetFineLimit, ":MS#"},
	}
	for _, c := range cases {
		m, f := newMount(map[string]string{c.want: "0"}) // slew started
		m.firmware = c.fw
		if err := c.call(m); err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
		if f.LastWrite() != c.want {
			t.Errorf("%s wrote %q, want %q", c.name, f.LastWrite(), c.want)
		}
	}
}

// TestParkFirmwareGating: modern firmware parks via :PsX# (ASCOM-saved position),
// falling back to the keypad park :hP# when none is saved ("3"); unknown/old firmware
// parks straight to :hP#.
func TestParkFirmwareGating(t *testing.T) {
	// Modern firmware, saved position present → :PsX# "0".
	m, f := newMount(map[string]string{":PsX#": "0#"})
	m.firmware = Version{3, 2, 5}
	if err := m.Park(); err != nil || f.LastWrite() != ":PsX#" {
		t.Errorf("Park(saved): %v wrote %q, want :PsX#", err, f.LastWrite())
	}

	// Modern firmware, no saved position ("3") → fall back to keypad park :hP#.
	m2, f2 := newMount(map[string]string{":PsX#": "3#"}) // :hP# is Blind (no scripted reply)
	m2.firmware = Version{3, 2, 5}
	if err := m2.Park(); err != nil || f2.LastWrite() != ":hP#" {
		t.Errorf("Park(fallback): %v last write %q, want :hP#", err, f2.LastWrite())
	}

	// Unknown firmware → keypad park directly.
	m3, f3 := newMount(nil)
	if err := m3.Park(); err != nil || f3.LastWrite() != ":hP#" {
		t.Errorf("Park(unknown fw): %v wrote %q, want :hP#", err, f3.LastWrite())
	}

	// "4" = already parked → success, no error.
	m4, _ := newMount(map[string]string{":PsX#": "4#"})
	m4.firmware = Version{3, 2, 5}
	if err := m4.Park(); err != nil {
		t.Errorf("Park(already parked): %v", err)
	}
}

// TestSlewingReflectsManualMove: a manual MoveAxis jog reads as slewing even though
// the :Ginfo# status flag (which tracks gotos) says idle.
func TestSlewingReflectsManualMove(t *testing.T) {
	m, _ := newMount(map[string]string{
		":Ginfo#": "6.0,30.0,E,90.0,45.0,2459580.5,0,0#", // status: not slewing
	})
	if sl, err := m.Slewing(); err != nil || sl {
		t.Fatalf("Slewing idle = %v, %v; want false", sl, err)
	}
	if err := m.MoveAxis(lx200.AxisPrimary, true, lx200.RateCenter); err != nil {
		t.Fatalf("MoveAxis: %v", err)
	}
	if sl, err := m.Slewing(); err != nil || !sl {
		t.Errorf("Slewing during jog = %v, %v; want true", sl, err)
	}
	if err := m.StopAxis(lx200.AxisPrimary); err != nil {
		t.Fatalf("StopAxis: %v", err)
	}
	if sl, err := m.Slewing(); err != nil || sl {
		t.Errorf("Slewing after stop = %v, %v; want false", sl, err)
	}
}

// TestMoveAxisRate: exact-rate manual slew sends :RA#/:RE# (deg/s, 6 decimals) plus
// the directional move; a zero rate stops the axis; the primary axis wraps the move in
// a speed-correction toggle.
func TestMoveAxisRate(t *testing.T) {
	// Primary axis, speed correction already off → no :SSC toggle.
	m, f := newMount(map[string]string{":GSC#": "0"})
	if err := m.MoveAxisRate(lx200.AxisPrimary, true, 1.5); err != nil {
		t.Fatalf("MoveAxisRate primary: %v", err)
	}
	if f.Count(":RA01.500000#") != 1 || f.LastWrite() != ":Me#" {
		t.Errorf("primary writes = %v", f.Writes())
	}

	// Primary axis, speed correction on → :SSC0# before, :SSC1# restored after.
	m2, f2 := newMount(map[string]string{":GSC#": "1", ":SSC0#": "1", ":SSC1#": "1"})
	if err := m2.MoveAxisRate(lx200.AxisPrimary, false, 2); err != nil {
		t.Fatalf("MoveAxisRate primary (corr on): %v", err)
	}
	w := f2.Writes()
	if f2.Count(":SSC0#") != 1 || f2.Count(":RA02.000000#") != 1 || f2.Count(":Mw#") != 1 || f2.LastWrite() != ":SSC1#" {
		t.Errorf("primary-with-correction writes = %v", w)
	}

	// Secondary axis → :RE#, no speed-correction toggle.
	m3, f3 := newMount(nil)
	if err := m3.MoveAxisRate(lx200.AxisSecondary, true, 3.25); err != nil {
		t.Fatalf("MoveAxisRate secondary: %v", err)
	}
	if f3.Count(":RE03.250000#") != 1 || f3.LastWrite() != ":Mn#" {
		t.Errorf("secondary writes = %v", f3.Writes())
	}

	// Zero rate → stop the axis (:Qn#/:Qs#).
	m4, f4 := newMount(nil)
	if err := m4.MoveAxisRate(lx200.AxisSecondary, true, 0); err != nil {
		t.Fatalf("MoveAxisRate stop: %v", err)
	}
	if f4.Count(":Qn#") != 1 {
		t.Errorf("stop writes = %v", f4.Writes())
	}

	// Out of field range → error, nothing sent.
	m5, f5 := newMount(nil)
	if err := m5.MoveAxisRate(lx200.AxisPrimary, true, 150); err == nil {
		t.Error("MoveAxisRate(150): want error")
	}
	if len(f5.Writes()) != 0 {
		t.Errorf("out-of-range wrote %v", f5.Writes())
	}
}

// TestPulseGuideTracking: PulseGuide reports through IsPulseGuiding until the pulse
// elapses, and SetGuideRate rejects unrepresentable rates.
func TestPulseGuideTracking(t *testing.T) {
	m, f := newMount(nil) // :Mg… is Blind (no reply)
	if m.IsPulseGuiding() {
		t.Error("IsPulseGuiding = true before any pulse")
	}
	if err := m.PulseGuide(lx200.North, 40); err != nil {
		t.Fatalf("PulseGuide: %v", err)
	}
	if f.LastWrite() != ":Mgn0040#" {
		t.Errorf("PulseGuide wrote %q, want :Mgn0040#", f.LastWrite())
	}
	if !m.IsPulseGuiding() {
		t.Error("IsPulseGuiding = false during pulse")
	}
	time.Sleep(120 * time.Millisecond)
	if m.IsPulseGuiding() {
		t.Error("IsPulseGuiding = true after pulse elapsed")
	}
}

func TestSetGuideRateRange(t *testing.T) {
	m, _ := newMount(nil)
	if err := m.SetGuideRate(150); err == nil {
		t.Error("SetGuideRate(150): want error")
	}
	if err := m.SetGuideRate(-1); err == nil {
		t.Error("SetGuideRate(-1): want error")
	}
}

// TestISODateParsing: in ultra-precision mode the mount returns ISO dates, which the
// getters must parse (they previously assumed MM/DD/YY and failed).
func TestISODateParsing(t *testing.T) {
	m, _ := newMount(map[string]string{
		":GUDT#": "2026-06-02,15:04:05#",
		":GLDT#": "2026-06-02,15:04:05#",
		":GC#":   "2026-06-02#",
	})
	want := time.Date(2026, 6, 2, 15, 4, 5, 0, time.UTC)
	if tm, err := m.UTCDateTime(); err != nil || !tm.Equal(want) {
		t.Errorf("UTCDateTime(ISO) = %v, %v; want %v", tm, err, want)
	}
	if tm, err := m.LocalDateTime(); err != nil || !tm.Equal(want) {
		t.Errorf("LocalDateTime(ISO) = %v, %v; want %v", tm, err, want)
	}
	if d, err := m.LocalDate(); err != nil || !d.Equal(time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("LocalDate(ISO) = %v, %v; want 2026-06-02", d, err)
	}
}
