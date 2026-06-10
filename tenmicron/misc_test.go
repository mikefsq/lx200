package tenmicron

import "testing"

func TestParallactic(t *testing.T) {
	m, _ := newMount(map[string]string{
		":GPA#":  "+045.00000#",
		":GPAS#": "+000.50000#",
		":GPAZ#": "-030.00000#",
	})
	if v, err := m.ParallacticAngle(); err != nil || v != 45 {
		t.Errorf("ParallacticAngle = %v, %v; want 45", v, err)
	}
	if v, err := m.ParallacticSpeed(); err != nil || v != 0.5 {
		t.Errorf("ParallacticSpeed = %v, %v; want 0.5", v, err)
	}
	if v, err := m.ParallacticAngleZenith(); err != nil || v != -30 {
		t.Errorf("ParallacticAngleZenith = %v, %v; want -30", v, err)
	}
}

func TestFinalApproachTimeConstant(t *testing.T) {
	m, _ := newMount(map[string]string{":GFAtc#": "1.50#"})
	if v, err := m.FinalApproachTimeConstant(); err != nil || v != 1.5 {
		t.Errorf("FinalApproachTimeConstant = %v, %v; want 1.5", v, err)
	}
	m2, _ := newMount(map[string]string{":GFAtc#": "E#"})
	if _, err := m2.FinalApproachTimeConstant(); err == nil {
		t.Errorf("FinalApproachTimeConstant(E#): want error")
	}
}

func TestLogs(t *testing.T) {
	m, f := newMount(map[string]string{":getlog#": "comm log#", ":evlog#": "event log#"})
	if err := m.StopCommLog(); err != nil || f.LastWrite() != ":stoplog#" {
		t.Errorf("StopCommLog: %v wrote %q", err, f.LastWrite())
	}
	if s, err := m.CommLog(); err != nil || s != "comm log" {
		t.Errorf("CommLog = %q, %v", s, err)
	}
	if s, err := m.EventLog(); err != nil || s != "event log" {
		t.Errorf("EventLog = %q, %v", s, err)
	}
}
