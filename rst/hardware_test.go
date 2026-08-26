package rst

import (
	"os"
	"testing"
)

// TestAgainstHardware exercises the accessors against a real mount. Skipped unless RST_HW is
// set, since it needs the mount connected and any other driver holding the port stopped.
// RST_PORT pins a port; empty means probe.
func TestAgainstHardware(t *testing.T) {
	if os.Getenv("RST_HW") == "" {
		t.Skip("set RST_HW=1 (and stop alpacahurd-device@rst) to run against real hardware")
	}
	var (
		m   *Mount
		err error
	)
	// With no port pinned this also exercises the probe path, which cannot be unit-tested
	// because it needs a real serial device to open and interrogate.
	if port := os.Getenv("RST_PORT"); port != "" {
		m, err = Open(port)
	} else {
		var rep Report
		m, rep, err = FindMatching(Filter{})
		if err == nil {
			t.Logf("probe          rejected %v", rep.Rejected)
			// Find is the no-filter wrapper over the same path; open and close it once so the
			// whole discovery chain is exercised.
			if m2, ferr := Find(); ferr == nil {
				m2.Close()
			} else {
				t.Logf("Find: %v", ferr)
			}
		}
	}
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer m.Close()

	if v, err := m.Version(); err != nil {
		t.Errorf("Version: %v", err)
	} else {
		t.Logf("firmware       %s", v)
	}
	if s, err := m.SerialNumber(); err != nil {
		t.Errorf("SerialNumber: %v", err)
	} else {
		t.Logf("serial         %s", s)
	}
	if ra, dec, err := m.GearRatio(); err != nil {
		t.Errorf("GearRatio: %v", err)
	} else {
		t.Logf("gear           %d / %d", ra, dec)
	}
	if ra, dec, err := m.WormCount(); err != nil {
		t.Errorf("WormCount: %v", err)
	} else {
		t.Logf("worm           %d / %d", ra, dec)
	}
	if at, err := m.AtHome(); err != nil {
		t.Errorf("AtHome: %v", err)
	} else {
		t.Logf("at home now    %v  (HomeFound %v)", at, m.HomeFound())
	}
	if p, err := m.Precision(); err != nil {
		t.Errorf("Precision: %v", err)
	} else if p != "H" && p != "L" {
		t.Errorf("Precision = %q; want H or L", p)
	} else {
		t.Logf("precision      %s", p)
	}
	if lim, err := m.SlewLimits(); err != nil {
		t.Errorf("SlewLimits: %v", err)
	} else {
		t.Logf("slew limits    %v", lim)
	}
	if ra, err := m.TargetRA(); err != nil {
		t.Errorf("TargetRA: %v", err)
	} else if dec, err := m.TargetDec(); err != nil {
		t.Errorf("TargetDec: %v", err)
	} else {
		t.Logf("target         RA %.4f  Dec %.4f", ra, dec)
	}
	if az, alt, err := m.TargetAltAz(); err != nil {
		t.Errorf("TargetAltAz: %v", err)
	} else {
		t.Logf("target alt/az  Az %.3f  Alt %.3f", az, alt)
	}
	if az, alt, err := m.polePosition(); err != nil {
		t.Errorf("polePosition: %v", err)
	} else {
		t.Logf("polar park     Az %.3f  Alt %.3f", az, alt)
	}
	if at, err := m.AtPark(); err != nil {
		t.Errorf("AtPark: %v", err)
	} else {
		t.Logf("at park        %v", at)
	}
	for i := 1; i <= 3; i++ {
		if n, err := m.SiteName(i); err != nil {
			t.Errorf("SiteName(%d): %v", i, err)
		} else {
			t.Logf("site %d         %q", i, n)
		}
	}
	if _, err := m.SiteName(4); err == nil {
		t.Error("SiteName(4) should error: :GP# is the precision mode, not a site name")
	}
}
