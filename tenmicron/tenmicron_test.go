package tenmicron

import (
	"testing"
	"time"

	"github.com/mikefsq/lx200"
)

// fake is an in-memory lx200.Transport: each command queues its scripted reply.
type fake struct {
	replies map[string]string
	writes  []string
	rbuf    []byte
}

func (f *fake) Write(p []byte) (int, error) {
	f.writes = append(f.writes, string(p))
	if r, ok := f.replies[string(p)]; ok {
		f.rbuf = append(f.rbuf, []byte(r)...)
	}
	return len(p), nil
}
func (f *fake) Read(p []byte) (int, error) {
	if len(f.rbuf) == 0 {
		return 0, nil
	}
	n := copy(p, f.rbuf)
	f.rbuf = f.rbuf[n:]
	return n, nil
}
func (f *fake) Close() error { return nil }

func newMount(replies map[string]string) (*Mount, *fake) {
	f := &fake{replies: replies}
	return &Mount{Conn: lx200.New(f, 200*time.Millisecond)}, f
}

func lastWrite(f *fake) string {
	if len(f.writes) == 0 {
		return ""
	}
	return f.writes[len(f.writes)-1]
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
		{GstatParked, false, false, true},
		{GstatSlewing, false, true, false},
		{GstatStopped, false, false, false},
		{GstatFollowingSat, true, false, false},
		{GstatUnknown, false, false, false}, // 98 -> idle (matches INDI)
		{GstatError, false, false, false},   // 99 -> idle
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
	n := 0
	for _, w := range f.writes {
		if w == ":Ginfo#" {
			n++
		}
	}
	if n != 1 {
		t.Errorf(":Ginfo# sent %d times, want 1 (cache coalescing)", n)
	}
}

func TestTrackingAndSiteCommands(t *testing.T) {
	m, f := newMount(map[string]string{
		":St+45*30:00#":  "1",
		":Sg-123*30:00#": "1", // East-positive 123.5 -> 10Micron East-negative
		":Sev+0100.0#":   "1",
		":Sdat1#":        "1",
	})

	if err := m.SetTracking(true); err != nil || lastWrite(f) != ":AP#" {
		t.Errorf("SetTracking(true): %v wrote %q", err, lastWrite(f))
	}
	if err := m.SetTracking(false); err != nil || lastWrite(f) != ":AL#" {
		t.Errorf("SetTracking(false): %v wrote %q", err, lastWrite(f))
	}
	if err := m.SetSiteLatitude(45.5); err != nil || lastWrite(f) != ":St+45*30:00#" {
		t.Errorf("SetSiteLatitude: %v wrote %q", err, lastWrite(f))
	}
	if err := m.SetSiteLongitude(123.5); err != nil || lastWrite(f) != ":Sg-123*30:00#" {
		t.Errorf("SetSiteLongitude: %v wrote %q", err, lastWrite(f))
	}
	if err := m.SetSiteElevation(100); err != nil || lastWrite(f) != ":Sev+0100.0#" {
		t.Errorf("SetSiteElevation: %v wrote %q", err, lastWrite(f))
	}
	if err := m.SetDualAxisTracking(true); err != nil || lastWrite(f) != ":Sdat1#" {
		t.Errorf("SetDualAxisTracking: %v wrote %q", err, lastWrite(f))
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
	if err := m.SetUTC(tm); err != nil || lastWrite(f) != ":SUDT2026-06-02,15:04:05#" {
		t.Errorf("SetUTC: %v wrote %q", err, lastWrite(f))
	}
	if err := m.SetUTCOffset(0); err != nil || lastWrite(f) != ":SG+00:00:00#" {
		t.Errorf("SetUTCOffset(0): %v wrote %q", err, lastWrite(f))
	}
	if err := m.SetUTCOffset(-5*time.Hour - 30*time.Minute); err != nil || lastWrite(f) != ":SG-05:30:00#" {
		t.Errorf("SetUTCOffset(-5:30): %v wrote %q", err, lastWrite(f))
	}
	if err := m.SetDate(tm); err != nil || lastWrite(f) != ":SC2026-06-02#" {
		t.Errorf("SetDate: %v wrote %q", err, lastWrite(f))
	}
	if err := m.SetTime(tm); err != nil || lastWrite(f) != ":SL15:04:05#" {
		t.Errorf("SetTime: %v wrote %q", err, lastWrite(f))
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
	f.replies[":Ginfo#"] = "6.0,30.0,E,90.0,45.0,2459580.5,1,0#" // now stopped
	if err := m.Halt(); err != nil || lastWrite(f) != ":Q#" {
		t.Fatalf("Halt: %v wrote %q", err, lastWrite(f))
	}
	if sl, err := m.Slewing(); err != nil || sl {
		t.Errorf("Slewing after Halt = %v, %v; want false (cache invalidated)", sl, err)
	}
}
