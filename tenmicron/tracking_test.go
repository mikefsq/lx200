package tenmicron

import "testing"

func TestSetTrackRate(t *testing.T) {
	cases := []struct {
		call func(*Mount) error
		want string
	}{
		{(*Mount).TrackLunar, ":RT0#"},
		{(*Mount).TrackSolar, ":RT1#"}, // NOT :TS# (the broken core form)
		{(*Mount).TrackSidereal, ":RT2#"},
		{func(m *Mount) error { return m.SetTrackRate(TrackStopped) }, ":RT9#"},
		{(*Mount).SelectCustomTrackRate, ":TM#"},
		{(*Mount).IncCustomTrackRate, ":T+#"},
		{(*Mount).DecCustomTrackRate, ":T-#"},
	}
	for _, c := range cases {
		m, f := newMount(nil)
		if err := c.call(m); err != nil {
			t.Errorf("call: %v", err)
		}
		if got := f.LastWrite(); got != c.want {
			t.Errorf("wrote %q, want %q", got, c.want)
		}
	}
}

func TestCustomAxisRates(t *testing.T) {
	m, f := newMount(map[string]string{":RR+000.5000#": "1", ":RD-000.2500#": "1"})
	if err := m.SetCustomRARate(0.5); err != nil || f.LastWrite() != ":RR+000.5000#" {
		t.Errorf("SetCustomRARate: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetCustomDecRate(-0.25); err != nil || f.LastWrite() != ":RD-000.2500#" {
		t.Errorf("SetCustomDecRate: %v wrote %q", err, f.LastWrite())
	}
}

func TestTrackingRateHz(t *testing.T) {
	m, _ := newMount(map[string]string{":GT#": "60.1#"})
	if v, err := m.TrackingRateHz(); err != nil || v != 60.1 {
		t.Errorf("TrackingRateHz = %v, %v; want 60.1", v, err)
	}
}

func TestTrackingActive(t *testing.T) {
	m, _ := newMount(map[string]string{":GTRK#": "1#"})
	if on, err := m.TrackingActive(); err != nil || !on {
		t.Errorf("TrackingActive = %v, %v; want true", on, err)
	}
	m2, _ := newMount(map[string]string{":GTRK#": "0#"})
	if on, _ := m2.TrackingActive(); on {
		t.Errorf("TrackingActive = true, want false")
	}
}

func TestDualAxisTracking(t *testing.T) {
	// :Gdat# replies a single bare status byte (no '#'), read via AckByte.
	m, _ := newMount(map[string]string{":Gdat#": "1"})
	if on, err := m.DualAxisTracking(); err != nil || !on {
		t.Errorf("DualAxisTracking = %v, %v; want true", on, err)
	}
	m2, _ := newMount(map[string]string{":Gdat#": "0"})
	if on, _ := m2.DualAxisTracking(); on {
		t.Errorf("DualAxisTracking = true, want false")
	}
}
