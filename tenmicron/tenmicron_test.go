package tenmicron

import (
	"testing"
	"time"

	"github.com/mikefsq/lx200"
	"github.com/mikefsq/lx200/internal/lx200test"
)

func newMount(replies map[string]string) (*Mount, *lx200test.Fake) {
	f := lx200test.New(replies)
	return &Mount{Conn: lx200.New(f, 200*time.Millisecond)}, f
}

func TestSetStatusTTL(t *testing.T) {
	m := &Mount{statusTTL: defaultStatusTTL}
	m.SetStatusTTL(2 * time.Second)
	if m.statusTTL != 2*time.Second {
		t.Errorf("statusTTL = %v, want 2s", m.statusTTL)
	}
	m.SetStatusTTL(0) // a poller can disable caching entirely
	if m.statusTTL != 0 {
		t.Errorf("statusTTL = %v, want 0", m.statusTTL)
	}
}

func TestParseGinfo(t *testing.T) {
	// RA(hours), Dec(deg), pier, Az, Alt, JD, Gstat, slew
	s, err := parseGinfo("12.500000,45.250000,W,180.00000,30.00000,2459580.5,6,1")
	if err != nil {
		t.Fatalf("parseGinfo: %v", err)
	}
	if s.RA != 12.5 || s.Dec != 45.25 || s.Az != 180 || s.Alt != 30 {
		t.Errorf("coords = %+v", s)
	}
	if s.Pier != lx200.PierWest {
		t.Errorf("pier = %v, want West", s.Pier)
	}
	if s.Gstat != GstatSlewing || !s.Slew {
		t.Errorf("Gstat=%d slew=%v", s.Gstat, s.Slew)
	}
	if !s.IsSlewing() || s.IsTracking() || s.IsParked() {
		t.Errorf("slewing-state flags wrong: %+v", s)
	}
	// Extra trailing fields must be tolerated.
	if _, err := parseGinfo("1,2,E,3,4,5,0,0,99,extra"); err != nil {
		t.Errorf("extra fields rejected: %v", err)
	}
}

func TestGstatMapping(t *testing.T) {
	cases := []struct {
		g                   int
		track, slew, parked bool
	}{
		{GstatTracking, true, false, false},
		{GstatParking, false, true, false},
		{GstatUnparking, false, true, false}, // 3: transitional motion -> slewing, not tracking
		{GstatParked, false, false, true},
		{GstatSlewing, false, true, false},
		{GstatStopped, false, false, false},
		{GstatOutOfLimits, true, false, false}, // 9: spec "tracking is on but outside limits"
		{GstatFollowingSat, true, false, false},
		{GstatDDSNoPower, false, false, false}, // 12: DDS controller waiting for power
		{GstatDDSMonitor, false, false, false}, // 13: DDS monitor mode
		{GstatDDSAutotune, false, true, false}, // 14: DDS autotune drives the axes
		{GstatUnknown, false, false, false},    // 98 -> idle (matches INDI)
		{GstatError, false, false, false},      // 99 -> idle
	}
	for _, c := range cases {
		s := Status{Gstat: c.g}
		if s.IsTracking() != c.track || s.IsSlewing() != c.slew || s.IsParked() != c.parked {
			t.Errorf("Gstat %d: track=%v slew=%v parked=%v, want %v/%v/%v",
				c.g, s.IsTracking(), s.IsSlewing(), s.IsParked(), c.track, c.slew, c.parked)
		}
	}
}

func TestStatusServedFromGinfo(t *testing.T) {
	m, f := newMount(map[string]string{
		":Ginfo#": "6.000000,30.000000,E,90.00000,45.00000,2459580.5,0,0#",
	})
	if ra, err := m.RA(); err != nil || ra != 6 {
		t.Errorf("RA = %v, %v", ra, err)
	}
	if dec, err := m.Dec(); err != nil || dec != 30 {
		t.Errorf("Dec = %v, %v", dec, err)
	}
	if tr, err := m.Tracking(); err != nil || !tr {
		t.Errorf("Tracking = %v, %v", tr, err)
	}
	if sl, _ := m.Slewing(); sl {
		t.Errorf("Slewing = true, want false")
	}
	if ps, _ := m.PierSide(); ps != lx200.PierEast {
		t.Errorf("PierSide = %v, want East", ps)
	}
	// The burst above must have hit :Ginfo# only once (cache TTL).
	if n := f.Count(":Ginfo#"); n != 1 {
		t.Errorf(":Ginfo# sent %d times, want 1 (cache coalescing)", n)
	}
}

func TestPierInversionSouthernOldFirmware(t *testing.T) {
	// Old firmware (< 2.15.32) + southern hemisphere → the reported side is flipped.
	m, _ := newMount(map[string]string{
		":Ginfo#": "6.0,-30.0,E,90.0,45.0,2459580.5,0,0#", // pier East
		":pS#":    "East#",
		":GTsid#": "3", // '3' = East (single byte)
	})
	m.firmware, m.southern = Version{2, 15, 0}, true
	if ps, _ := m.PierSide(); ps != lx200.PierWest {
		t.Errorf("PierSide (old fw, south) = %v; want inverted West", ps)
	}
	if ps, _ := m.PointingState(); ps != lx200.PierWest {
		t.Errorf("PointingState (old fw, south) = %v; want inverted West", ps)
	}
	if ps, _ := m.DestinationSideOfPier(); ps != lx200.PierWest {
		t.Errorf("DestinationSideOfPier (old fw, south) = %v; want inverted West", ps)
	}

	// Modern firmware → no inversion even in the south.
	m2, _ := newMount(map[string]string{":pS#": "East#"})
	m2.firmware, m2.southern = Version{3, 3, 4}, true
	if ps, _ := m2.PointingState(); ps != lx200.PierEast {
		t.Errorf("PointingState (modern fw) = %v; want East (no inversion)", ps)
	}

	// Old firmware but northern hemisphere → no inversion.
	m3, _ := newMount(map[string]string{":pS#": "East#"})
	m3.firmware, m3.southern = Version{2, 15, 0}, false
	if ps, _ := m3.PointingState(); ps != lx200.PierEast {
		t.Errorf("PointingState (north, old fw) = %v; want East (no inversion)", ps)
	}
}

func TestTrackingAndSiteCommands(t *testing.T) {
	m, f := newMount(map[string]string{
		":St+45*30:00#":  "1",
		":Sg-123*30:00#": "1", // East-positive 123.5 -> 10Micron East-negative
		":Sev+0100.0#":   "1",
		":Sdat1#":        "1",
	})

	if err := m.SetTracking(true); err != nil || f.LastWrite() != ":AP#" {
		t.Errorf("SetTracking(true): %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetTracking(false); err != nil || f.LastWrite() != ":AL#" {
		t.Errorf("SetTracking(false): %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetSiteLatitude(45.5); err != nil || f.LastWrite() != ":St+45*30:00#" {
		t.Errorf("SetSiteLatitude: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetSiteLongitude(123.5); err != nil || f.LastWrite() != ":Sg-123*30:00#" {
		t.Errorf("SetSiteLongitude: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetSiteElevation(100); err != nil || f.LastWrite() != ":Sev+0100.0#" {
		t.Errorf("SetSiteElevation: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetDualAxisTracking(true); err != nil || f.LastWrite() != ":Sdat1#" {
		t.Errorf("SetDualAxisTracking: %v wrote %q", err, f.LastWrite())
	}
}

func TestDateTimeSetters(t *testing.T) {
	m, f := newMount(map[string]string{
		":SUDT2026-06-02,15:04:05#": "1",
		":SG+00:00:00#":             "1",
		":SG-05:30:00#":             "1",
		":SC2026-06-02#":            "1",
		":SL15:04:05#":              "1",
	})
	tm := time.Date(2026, 6, 2, 15, 4, 5, 0, time.UTC)
	if err := m.SetUTC(tm); err != nil || f.LastWrite() != ":SUDT2026-06-02,15:04:05#" {
		t.Errorf("SetUTC: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetUTCOffset(0); err != nil || f.LastWrite() != ":SG+00:00:00#" {
		t.Errorf("SetUTCOffset(0): %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetUTCOffset(-5*time.Hour - 30*time.Minute); err != nil || f.LastWrite() != ":SG-05:30:00#" {
		t.Errorf("SetUTCOffset(-5:30): %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetDate(tm); err != nil || f.LastWrite() != ":SC2026-06-02#" {
		t.Errorf("SetDate: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetTime(tm); err != nil || f.LastWrite() != ":SL15:04:05#" {
		t.Errorf("SetTime: %v wrote %q", err, f.LastWrite())
	}
}

// TestHaltInvalidatesCache: after an abort the next Slewing() must refetch
// rather than report the stale (slewing) cached status.
func TestHaltInvalidatesCache(t *testing.T) {
	m, f := newMount(map[string]string{
		":Ginfo#": "6.0,30.0,E,90.0,45.0,2459580.5,6,1#", // slewing
	})
	if sl, err := m.Slewing(); err != nil || !sl {
		t.Fatalf("Slewing = %v, %v; want true", sl, err)
	}
	f.SetReply(":Ginfo#", "6.0,30.0,E,90.0,45.0,2459580.5,1,0#") // now stopped
	if err := m.Halt(); err != nil || f.LastWrite() != ":Q#" {
		t.Fatalf("Halt: %v wrote %q", err, f.LastWrite())
	}
	if sl, err := m.Slewing(); err != nil || sl {
		t.Errorf("Slewing after Halt = %v, %v; want false (cache invalidated)", sl, err)
	}
}
