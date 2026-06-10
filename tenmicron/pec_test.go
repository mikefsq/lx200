package tenmicron

import "testing"

func TestPEC(t *testing.T) {
	cases := []struct {
		call func(*Mount) error
		want string
	}{
		{(*Mount).StopPEC, ":p#"},
		{(*Mount).StartPEC, ":pP#"},
		{(*Mount).TrainPEC, ":pR#"},
		{func(m *Mount) error { return m.TrainPECLength(PECMedium) }, ":pR1#"},
		{func(m *Mount) error { return m.TrainPECAltitude(PECShort) }, ":pRa0#"},
		{func(m *Mount) error { return m.TrainPECAzimuth(PECLong) }, ":pRz2#"},
	}
	for _, c := range cases {
		m, f := newMount(nil)
		if err := c.call(m); err != nil || f.LastWrite() != c.want {
			t.Errorf("wrote %q, want %q (%v)", f.LastWrite(), c.want, err)
		}
	}
}
