package tenmicron

import (
	"testing"
	"time"

	"github.com/mikefsq/lx200"
)

func TestStatusGetters(t *testing.T) {
	m, _ := newMount(map[string]string{
		":Gstat#": "6#",
		":Gmte#":  "120#",
		":Gstm#":  "5.000#",
		":Gh#":    "+85#",
		":Go#":    "+00*#", // low/high-precision '*' tolerated
		":GMF#":   "1#",
		":Glmt#":  "5#",
		":Glms#":  "10#",
		":Guaf#":  "1#",
	})
	if v, err := m.StatusCode(); err != nil || v != 6 {
		t.Errorf("StatusCode = %v, %v; want 6", v, err)
	}
	if d, err := m.TimeToTrackingEnd(); err != nil || d != 120*time.Minute {
		t.Errorf("TimeToTrackingEnd = %v, %v; want 120m", d, err)
	}
	if d, err := m.SlewSettleTime(); err != nil || d != 5*time.Second {
		t.Errorf("SlewSettleTime = %v, %v; want 5s", d, err)
	}
	if v, err := m.HighAltitudeLimit(); err != nil || v != 85 {
		t.Errorf("HighAltitudeLimit = %v, %v; want 85", v, err)
	}
	if v, err := m.LowAltitudeLimit(); err != nil || v != 0 {
		t.Errorf("LowAltitudeLimit = %v, %v; want 0", v, err)
	}
	if v, err := m.MeridianSideBehaviour(); err != nil || v != MeridianBothSides {
		t.Errorf("MeridianSideBehaviour = %v, %v; want 1", v, err)
	}
	if v, err := m.MeridianTrackLimit(); err != nil || v != 5 {
		t.Errorf("MeridianTrackLimit = %v, %v; want 5", v, err)
	}
	if v, err := m.MeridianSlewLimit(); err != nil || v != 10 {
		t.Errorf("MeridianSlewLimit = %v, %v; want 10", v, err)
	}
	if v, err := m.UnattendedFlip(); err != nil || !v {
		t.Errorf("UnattendedFlip = %v, %v; want true", v, err)
	}
}

func TestLimitSetters(t *testing.T) {
	m, f := newMount(map[string]string{":Shs+85#": "1", ":Sos-05#": "1", ":SMF2#": "1"})
	if ok, err := m.SetHighAltitudeLimit(85); err != nil || !ok || f.LastWrite() != ":Shs+85#" {
		t.Errorf("SetHighAltitudeLimit: ok=%v err=%v wrote %q", ok, err, f.LastWrite())
	}
	if ok, err := m.SetLowAltitudeLimit(-5); err != nil || !ok || f.LastWrite() != ":Sos-05#" {
		t.Errorf("SetLowAltitudeLimit: ok=%v err=%v wrote %q", ok, err, f.LastWrite())
	}
	if ok, err := m.SetMeridianSideBehaviour(MeridianWestOnly); err != nil || !ok || f.LastWrite() != ":SMF2#" {
		t.Errorf("SetMeridianSideBehaviour: ok=%v err=%v wrote %q", ok, err, f.LastWrite())
	}
}

func TestDestinationSideOfPier(t *testing.T) {
	for reply, want := range map[string]lx200.PierSide{ // single bare digit, no '#'
		"2": lx200.PierWest,
		"3": lx200.PierEast,
		"0": lx200.PierUnknown,
	} {
		m, _ := newMount(map[string]string{":GTsid#": reply})
		if ps, err := m.DestinationSideOfPier(); err != nil || ps != want {
			t.Errorf("DestinationSideOfPier(%q) = %v, %v; want %v", reply, ps, err, want)
		}
	}
}
