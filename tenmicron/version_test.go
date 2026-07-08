package tenmicron

import "testing"

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in            string
		maj, min, fix int
		ok            bool
	}{
		{"3.2.5#", 3, 2, 5, true},
		{"2.11", 2, 11, 0, true}, // .fix optional → 0
		{" 2.15.32 ", 2, 15, 32, true},
		{"garbage", 0, 0, 0, false},
		{"3", 0, 0, 0, false}, // needs at least maj.min
	}
	for _, c := range cases {
		v, err := parseVersion(c.in)
		if (err == nil) != c.ok {
			t.Errorf("parseVersion(%q) err=%v, want ok=%v", c.in, err, c.ok)
			continue
		}
		if c.ok && (v.Major != c.maj || v.Minor != c.min || v.Fix != c.fix) {
			t.Errorf("parseVersion(%q) = %s, want %d.%d.%d", c.in, v, c.maj, c.min, c.fix)
		}
	}
}

func TestVersionAtLeast(t *testing.T) {
	v := Version{2, 11, 0}
	for _, lo := range [][3]int{{2, 11, 0}, {2, 10, 9}, {1, 99, 99}} {
		if !v.atLeast(lo[0], lo[1], lo[2]) {
			t.Errorf("%s.atLeast%v = false, want true", v, lo)
		}
	}
	for _, hi := range [][3]int{{2, 11, 1}, {2, 12, 0}, {3, 0, 0}} {
		if v.atLeast(hi[0], hi[1], hi[2]) {
			t.Errorf("%s.atLeast%v = true, want false", v, hi)
		}
	}
	if (Version{}).atLeast(1, 0, 0) { // unknown firmware compares below everything
		t.Error("zero Version.atLeast(1.0.0) = true, want false")
	}
}

func TestFirmwareVersion(t *testing.T) {
	m := &Mount{firmware: Version{3, 2, 5}}
	if got := m.FirmwareVersion(); got != (Version{3, 2, 5}) {
		t.Errorf("FirmwareVersion() = %s, want 3.2.5", got)
	}
	if got := (&Mount{}).FirmwareVersion(); got != (Version{}) {
		t.Errorf("FirmwareVersion() on bare Mount = %s, want zero", got)
	}
	// FirmwareAtLeast is the public wrapper over Version.atLeast.
	if !m.FirmwareAtLeast(3, 2, 5) {
		t.Error("FirmwareAtLeast(3.2.5) = false, want true")
	}
	if m.FirmwareAtLeast(3, 2, 6) {
		t.Error("FirmwareAtLeast(3.2.6) = true, want false")
	}
	if (&Mount{}).FirmwareAtLeast(1, 0, 0) { // unknown firmware → false
		t.Error("bare Mount FirmwareAtLeast(1.0.0) = true, want false")
	}
}
