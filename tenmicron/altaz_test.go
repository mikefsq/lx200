package tenmicron

import (
	"math"
	"testing"
)

func TestSetAltAzTarget(t *testing.T) {
	m, f := newMount(map[string]string{":Sa+45*30:00#": "1", ":Sz123*30:00#": "1"})
	if ok, err := m.SetTargetAltitude(45.5); err != nil || !ok {
		t.Errorf("SetTargetAltitude: ok=%v err=%v wrote %q", ok, err, f.LastWrite())
	}
	if f.LastWrite() != ":Sa+45*30:00#" {
		t.Errorf("SetTargetAltitude wrote %q", f.LastWrite())
	}
	if ok, err := m.SetTargetAzimuth(123.5); err != nil || !ok {
		t.Errorf("SetTargetAzimuth: ok=%v err=%v", ok, err)
	}
	if f.LastWrite() != ":Sz123*30:00#" {
		t.Errorf("SetTargetAzimuth wrote %q", f.LastWrite())
	}
}

func TestSlewToAltAz(t *testing.T) {
	m, f := newMount(map[string]string{
		":Sa+45*30:00#": "1",
		":Sz123*30:00#": "1",
		":MA#":          "0", // slew started
	})
	if err := m.SlewToAltAz(45.5, 123.5); err != nil {
		t.Fatalf("SlewToAltAz: %v", err)
	}
	if f.LastWrite() != ":MA#" {
		t.Errorf("final write %q, want :MA#", f.LastWrite())
	}
}

func TestSlewToAltAzRejected(t *testing.T) {
	m, _ := newMount(map[string]string{":Sa-10*00:00#": "0"}) // out of range
	if err := m.SlewToAltAz(-10, 0); err == nil {
		t.Errorf("SlewToAltAz below range: want error")
	}
}

func TestTargetGetters(t *testing.T) {
	m, _ := newMount(map[string]string{
		":Gr#": "20:30:00.0#",
		":Gd#": "-52:00:00.0#",
		":Ga#": "+45:30:00.0#",
		":Gz#": "123:30:00.0#",
	})
	if v, err := m.TargetRA(); err != nil || math.Abs(v-20.5) > 1e-6 {
		t.Errorf("TargetRA = %v, %v", v, err)
	}
	if v, err := m.TargetDec(); err != nil || math.Abs(v-(-52)) > 1e-6 {
		t.Errorf("TargetDec = %v, %v", v, err)
	}
	if v, err := m.TargetAltitude(); err != nil || math.Abs(v-45.5) > 1e-6 {
		t.Errorf("TargetAltitude = %v, %v", v, err)
	}
	if v, err := m.TargetAzimuth(); err != nil || math.Abs(v-123.5) > 1e-6 {
		t.Errorf("TargetAzimuth = %v, %v", v, err)
	}
}
