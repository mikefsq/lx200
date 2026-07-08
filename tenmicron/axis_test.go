package tenmicron

import (
	"math"
	"testing"
)

func TestAxisAngles(t *testing.T) {
	m, _ := newMount(map[string]string{
		":GaXa#": "+045.1234#",
		":GaXb#": "-010.0000#",
		":QaXa#": "+045.1234#",
	})
	if v, err := m.AxisAnglePrimary(); err != nil || math.Abs(v-45.1234) > 1e-6 {
		t.Errorf("AxisAnglePrimary = %v, %v", v, err)
	}
	if v, err := m.AxisAngleSecondary(); err != nil || math.Abs(v-(-10)) > 1e-6 {
		t.Errorf("AxisAngleSecondary = %v, %v", v, err)
	}
	if v, err := m.TargetAxisAnglePrimary(); err != nil || math.Abs(v-45.1234) > 1e-6 {
		t.Errorf("TargetAxisAnglePrimary = %v, %v", v, err)
	}
}

func TestSetAxisTargetAndSlew(t *testing.T) {
	m, f := newMount(map[string]string{
		":SaXa+045.1234#": "1",
		":MaX#":           "0",
	})
	if ok, err := m.SetTargetAxisAnglePrimary(45.1234); err != nil || !ok || f.LastWrite() != ":SaXa+045.1234#" {
		t.Errorf("SetTargetAxisAnglePrimary: ok=%v err=%v wrote %q", ok, err, f.LastWrite())
	}
	if err := m.SlewToAxisTarget(); err != nil || f.LastWrite() != ":MaX#" {
		t.Errorf("SlewToAxisTarget: %v wrote %q", err, f.LastWrite())
	}
}

func TestParkPositions(t *testing.T) {
	m, _ := newMount(map[string]string{":PiP#": "1#"})
	if err := m.ParkInPlace(); err != nil {
		t.Errorf("ParkInPlace: %v", err)
	}
	m2, _ := newMount(map[string]string{":PiP#": "0#"})
	if err := m2.ParkInPlace(); err == nil {
		t.Errorf("ParkInPlace(0#): want error")
	}
	m3, _ := newMount(map[string]string{":PsX#": "0#"})
	if err := m3.ParkToSaved(); err != nil {
		t.Errorf("ParkToSaved(0#): %v", err)
	}
	m4, _ := newMount(map[string]string{":PaX#": "1#"})
	if err := m4.SlewToAxisTargetAndPark(); err == nil {
		t.Errorf("SlewToAxisTargetAndPark(1#): want below-limit error")
	}
	m5, f5 := newMount(map[string]string{":PyX#": "1"}) // single bare byte, no '#'; '1' = ok
	if err := m5.SaveParkPosition(); err != nil || f5.LastWrite() != ":PyX#" {
		t.Errorf("SaveParkPosition: %v wrote %q", err, f5.LastWrite())
	}
	m6, _ := newMount(map[string]string{":PyX#": "0"}) // vendor driver: non-'1' = failure
	if err := m6.SaveParkPosition(); err == nil {
		t.Errorf("SaveParkPosition(0): want error")
	}
}

func TestSlewToRAAxis(t *testing.T) {
	// Slews to the RA-axis reference: primary(RA)=90°, secondary(Dec)=0°, then :MaX#
	// (slew-and-STOP, not park).
	m, f := newMount(map[string]string{
		":SaXa+090.0000#": "1",
		":SaXb+000.0000#": "1",
		":MaX#":           "0",
	})
	if err := m.SlewToRAAxis(); err != nil {
		t.Errorf("SlewToRAAxis: %v", err)
	}
	if f.LastWrite() != ":MaX#" {
		t.Errorf("last write = %q, want :MaX#", f.LastWrite())
	}
	// A rejected primary target must surface as an error.
	m2, _ := newMount(map[string]string{":SaXa+090.0000#": "0"})
	if err := m2.SlewToRAAxis(); err == nil {
		t.Error("SlewToRAAxis: primary rejected, want error")
	}
}

func TestRotateRAAxis(t *testing.T) {
	// Rotates only the RA axis: reads current Dec-axis angle, re-targets it unchanged.
	m, f := newMount(map[string]string{
		":GaXb#":          "+012.3400#",
		":SaXa+070.0000#": "1",
		":SaXb+012.3400#": "1",
		":MaX#":           "0",
	})
	if err := m.RotateRAAxis(70); err != nil {
		t.Errorf("RotateRAAxis: %v", err)
	}
	if f.LastWrite() != ":MaX#" {
		t.Errorf("last write = %q, want :MaX#", f.LastWrite())
	}
}
