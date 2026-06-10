package tenmicron

import "testing"

func TestFocuserQueries(t *testing.T) {
	m, _ := newMount(map[string]string{
		":FocQmax#": "2#",
		":FocQ1#":   "1#",
		":FocI1#":   "MyFoc,linear,SN123#",
		":FocGuF1#": "+001500#",
		":Focd1#":   "3#",
		":FocD1#":   "1#",
		":FocT1#":   "+12.5#",
		":FocGZ1#":  "+00000,+50000#",
		":FocGms1#": "01000#",
	})
	if n, err := m.FocuserMaxIndex(); err != nil || n != 2 {
		t.Errorf("FocuserMaxIndex = %d, %v; want 2", n, err)
	}
	f := m.Focuser(1)
	if ok, err := f.Available(); err != nil || !ok {
		t.Errorf("Available = %v, %v; want true", ok, err)
	}
	if fi, err := f.Info(); err != nil || fi.Name != "MyFoc" || fi.Type != "linear" || fi.Serial != "SN123" {
		t.Errorf("Info = %+v, %v", fi, err)
	}
	if p, err := f.Position(); err != nil || p != 1500 {
		t.Errorf("Position = %d, %v; want 1500", p, err)
	}
	if st, err := f.Status(); err != nil || st != FocuserSlewing {
		t.Errorf("Status = %v, %v; want slewing", st, err)
	}
	if mv, err := f.Moving(); err != nil || !mv {
		t.Errorf("Moving = %v, %v; want true", mv, err)
	}
	if tC, err := f.Temperature(); err != nil || tC != 12.5 {
		t.Errorf("Temperature = %v, %v; want 12.5", tC, err)
	}
	if lo, hi, err := f.Range(); err != nil || lo != 0 || hi != 50000 {
		t.Errorf("Range = %d,%d,%v; want 0,50000", lo, hi, err)
	}
	if sp, err := f.MaxSpeed(); err != nil || sp != 1000 {
		t.Errorf("MaxSpeed = %d, %v; want 1000", sp, err)
	}
}

func TestFocuserCommands(t *testing.T) {
	m, ff := newMount(map[string]string{
		":FocSuF1,+002000#":      "1",
		":FocSS1#":               "1",
		":FocSZ1,+00000,+60000#": "1",
		":FocHS1#":               "1",
	})
	f := m.Focuser(1)
	if ok, err := f.SetDestination(2000); err != nil || !ok || ff.LastWrite() != ":FocSuF1,+002000#" {
		t.Errorf("SetDestination: ok=%v err=%v wrote %q", ok, err, ff.LastWrite())
	}
	if ok, err := f.StartMove(); err != nil || !ok {
		t.Errorf("StartMove = %v, %v; want true", ok, err)
	}
	if ok, err := f.SetRange(0, 60000); err != nil || !ok || ff.LastWrite() != ":FocSZ1,+00000,+60000#" {
		t.Errorf("SetRange: ok=%v err=%v wrote %q", ok, err, ff.LastWrite())
	}
	if ok, err := f.StartHoming(); err != nil || !ok {
		t.Errorf("StartHoming = %v, %v; want true", ok, err)
	}
	if err := f.Stop(); err != nil || ff.LastWrite() != ":FocSq1#" {
		t.Errorf("Stop: %v wrote %q", err, ff.LastWrite())
	}
}

func TestFocuser1Legacy(t *testing.T) {
	cases := []struct {
		call func(*Mount) error
		want string
	}{
		{(*Mount).Focuser1In, ":F+#"},
		{(*Mount).Focuser1Out, ":F-#"},
		{(*Mount).Focuser1SpeedFast, ":FF#"},
		{(*Mount).Focuser1SpeedSlow, ":FS#"},
		{(*Mount).Focuser1Halt, ":FQ#"},
		{func(m *Mount) error { return m.Focuser1Speed(2) }, ":F2#"},
	}
	for _, c := range cases {
		m, f := newMount(nil)
		if err := c.call(m); err != nil || f.LastWrite() != c.want {
			t.Errorf("wrote %q, want %q (%v)", f.LastWrite(), c.want, err)
		}
	}
}
