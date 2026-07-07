package tenmicron

import "testing"

// TestUltraPrecisionTargets locks in the fractional-second target formats the mount
// accepts in the :U2# ultra-precision mode Connect forces: without them a target is
// quantised to whole seconds (~15" RA, ~1" Dec/Alt).
func TestUltraPrecisionTargets(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		call func(*Mount) (bool, error)
	}{
		{"RA", ":Sr20:30:00.00#", func(m *Mount) (bool, error) { return m.SetTargetRA(20.5) }},
		{"Dec+", ":Sd+45*30:00.0#", func(m *Mount) (bool, error) { return m.SetTargetDec(45.5) }},
		{"Dec-", ":Sd-05*15:00.0#", func(m *Mount) (bool, error) { return m.SetTargetDec(-5.25) }},
		{"Alt", ":Sa+45*30:00.0#", func(m *Mount) (bool, error) { return m.SetTargetAltitude(45.5) }},
		{"Az", ":Sz123*30:00.0#", func(m *Mount) (bool, error) { return m.SetTargetAzimuth(123.5) }},
	}
	for _, c := range cases {
		m, f := newMount(map[string]string{c.cmd: "1"})
		if ok, err := c.call(m); err != nil || !ok {
			t.Errorf("%s: ok=%v err=%v (wrote %q)", c.name, ok, err, f.LastWrite())
		}
		if f.LastWrite() != c.cmd {
			t.Errorf("%s wrote %q, want %q", c.name, f.LastWrite(), c.cmd)
		}
	}
}

// TestEncodeSexRounding checks the half-up carry: a value just shy of a second
// boundary must round up (and carry through minutes/hours), not truncate down.
func TestEncodeSexRounding(t *testing.T) {
	if got := hmsPrec(12.499999, 2); got != "12:30:00.00" { // 12:29:59.9964 → carry
		t.Errorf("hmsPrec(12.499999,2) = %q, want 12:30:00.00", got)
	}
	if got := hmsPrec(-0.0001, 2); got != "23:59:59.64" { // wraps into [0,24)
		t.Errorf("hmsPrec(-0.0001,2) = %q, want 23:59:59.64", got)
	}
	if got := dmsPrec(-0.5, 2, 1, true); got != "-00*30:00.0" {
		t.Errorf("dmsPrec(-0.5) = %q, want -00*30:00.0", got)
	}
}
