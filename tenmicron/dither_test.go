package tenmicron

import "testing"

func TestDitheringControls(t *testing.T) {
	m, f := newMount(map[string]string{
		":SditS#":     "1#",
		":SditQ#":     "0#",
		":SditN#":     "1#",
		":GditS#":     "1#",
		":SditM10,5#": "1#",
	})
	if ok, err := m.StartDithering(); err != nil || !ok {
		t.Errorf("StartDithering = %v, %v; want true", ok, err)
	}
	if ok, _ := m.StopDithering(); ok {
		t.Errorf("StopDithering = true, want false (0#)")
	}
	if ok, _ := m.DitherNow(); !ok {
		t.Errorf("DitherNow = false, want true")
	}
	if ok, _ := m.DitheringActive(); !ok {
		t.Errorf("DitheringActive = false, want true")
	}
	if ok, err := m.SetDitherAmount(10, 5); err != nil || !ok || f.LastWrite() != ":SditM10,5#" {
		t.Errorf("SetDitherAmount: ok=%v err=%v wrote %q", ok, err, f.LastWrite())
	}
}

func TestDitherParameters(t *testing.T) {
	m, _ := newMount(map[string]string{":GditP#": "10,5,2,30,60#"})
	p, err := m.DitherParameters()
	if err != nil || p.RAArcsec != 10 || p.DecArcsec != 5 || p.DelaySec != 2 ||
		p.ExposureSec != 30 || p.IntervalSec != 60 {
		t.Errorf("DitherParameters = %+v, %v", p, err)
	}
}
