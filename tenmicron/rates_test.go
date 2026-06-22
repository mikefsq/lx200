package tenmicron

import "testing"

func TestRateIndexSetters(t *testing.T) {
	cases := []struct {
		call func(*Mount) error
		want string
	}{
		{func(m *Mount) error { return m.SetGuidingRateIndex(2) }, ":RG2#"},
		{func(m *Mount) error { return m.SetCenteringRateIndex(3) }, ":RC3#"},
		{func(m *Mount) error { return m.SetSlewRateIndex(0) }, ":RS0#"},
		{func(m *Mount) error { return m.SetGuiderPortEnabled(true) }, ":Sge1#"},
		{func(m *Mount) error { return m.SetGuideRate(7.5) }, ":Rg07.5#"},  // in band, unchanged
		{func(m *Mount) error { return m.SetGuideRate(20.0) }, ":Rg15.0#"}, // > 1.0× -> clamped to sidereal
		{func(m *Mount) error { return m.SetGuideRate(0.5) }, ":Rg01.5#"},  // < 0.1× -> clamped to floor
		{func(m *Mount) error { return m.SetGuideRate(-3) }, ":Rg01.5#"},   // negative -> floor
	}
	for _, c := range cases {
		m, f := newMount(nil)
		if err := c.call(m); err != nil {
			t.Errorf("%s: %v", c.want, err)
		}
		if f.LastWrite() != c.want {
			t.Errorf("wrote %q, want %q", f.LastWrite(), c.want)
		}
	}
}

func TestSetMaxSlewRate(t *testing.T) {
	m, f := newMount(map[string]string{":Sw3#": "1"})
	if ok, err := m.SetMaxSlewRate(3); err != nil || !ok || f.LastWrite() != ":Sw3#" {
		t.Errorf("SetMaxSlewRate: ok=%v err=%v wrote %q", ok, err, f.LastWrite())
	}
}

func TestRateGetters(t *testing.T) {
	m, _ := newMount(map[string]string{
		":GMs#":  "5.0#",
		":GMsa#": "2.0#",
		":GMsb#": "10.0#",
		":Ggui#": "7.50#",
		":Gpgc#": "1#",
	})
	for _, c := range []struct {
		got  func() (float64, error)
		want float64
		name string
	}{
		{m.SlewRate, 5.0, "SlewRate"},
		{m.MinSlewRate, 2.0, "MinSlewRate"},
		{m.MaxSlewRate, 10.0, "MaxSlewRate"},
		{m.GuideRate, 7.5, "GuideRate"},
	} {
		if v, err := c.got(); err != nil || v != c.want {
			t.Errorf("%s = %v, %v; want %v", c.name, v, err, c.want)
		}
	}
	if gs, err := m.GuidingState(); err != nil || gs != 1 {
		t.Errorf("GuidingState = %v, %v; want 1", gs, err)
	}
}
