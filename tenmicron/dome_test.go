package tenmicron

import (
	"testing"
	"time"
)

func TestDomeStatus(t *testing.T) {
	m, _ := newMount(map[string]string{
		":GDA#":   "1805#",
		":GDF#":   "2#",
		":GDS#":   "3#",
		":GDH#":   "1#",
		":GDW#":   "0#",
		":GDstm#": "3.000#",
	})
	if v, err := m.DomeAzimuth(); err != nil || v != 180.5 {
		t.Errorf("DomeAzimuth = %v, %v; want 180.5", v, err)
	}
	if v, err := m.DomeFlap(); err != nil || v != DomeFlapOpen {
		t.Errorf("DomeFlap = %v, %v; want open", v, err)
	}
	if v, err := m.DomeShutter(); err != nil || v != DomeShutterMoving {
		t.Errorf("DomeShutter = %v, %v; want moving", v, err)
	}
	if v, err := m.DomeHoming(); err != nil || !v {
		t.Errorf("DomeHoming = %v, %v; want true", v, err)
	}
	if v, err := m.DomeSlewing(); err != nil || v {
		t.Errorf("DomeSlewing = %v, %v; want false", v, err)
	}
	if d, err := m.DomeSettleTime(); err != nil || d != 3*time.Second {
		t.Errorf("DomeSettleTime = %v, %v; want 3s", d, err)
	}
}

func TestDomeNoDome(t *testing.T) {
	m, _ := newMount(map[string]string{":GDA#": "9999#"})
	if _, err := m.DomeAzimuth(); err == nil {
		t.Errorf("DomeAzimuth(9999): want error")
	}
}

func TestDomeCommands(t *testing.T) {
	m, f := newMount(map[string]string{
		":SDF2#": "1#", ":SDS1#": "1#", ":SDH#": "1#", ":SDM1#": "1#",
	})
	if ok, err := m.CommandDomeFlap(true); err != nil || !ok || f.LastWrite() != ":SDF2#" {
		t.Errorf("CommandDomeFlap(open): ok=%v err=%v wrote %q", ok, err, f.LastWrite())
	}
	if ok, err := m.CommandDomeShutter(false); err != nil || !ok || f.LastWrite() != ":SDS1#" {
		t.Errorf("CommandDomeShutter(close): ok=%v err=%v wrote %q", ok, err, f.LastWrite())
	}
	if ok, err := m.StartDomeHoming(); err != nil || !ok {
		t.Errorf("StartDomeHoming = %v, %v; want true", ok, err)
	}
	if ok, err := m.SetDomeControl(DomeOnRS232); err != nil || !ok || f.LastWrite() != ":SDM1#" {
		t.Errorf("SetDomeControl: ok=%v err=%v wrote %q", ok, err, f.LastWrite())
	}
	if err := m.SetDomeMountType(1); err != nil || f.LastWrite() != ":SDT1#" {
		t.Errorf("SetDomeMountType: %v wrote %q", err, f.LastWrite())
	}
}
