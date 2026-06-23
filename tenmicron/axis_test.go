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
	m5, f5 := newMount(map[string]string{":PyX#": "0"}) // single bare byte, no '#'
	if err := m5.SaveParkPosition(); err != nil || f5.LastWrite() != ":PyX#" {
		t.Errorf("SaveParkPosition: %v wrote %q", err, f5.LastWrite())
	}
}
