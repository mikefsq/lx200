package onstep

import (
	"testing"
	"time"

	"github.com/mikefsq/lx200"
	"github.com/mikefsq/lx200/internal/lx200test"
)

func mount(replies map[string]string) (*Mount, *lx200test.Fake) {
	f := lx200test.New(replies)
	return &Mount{Conn: lx200.New(f, 200*time.Millisecond)}, f
}

func TestParseGU(t *testing.T) {
	cases := []struct {
		gu                               string
		slew, track, parked, home, altaz bool
	}{
		{"N", false, true, false, false, false},   // tracking
		{"n", true, false, false, false, false},   // slewing
		{"nN", false, false, false, false, false}, // idle (both)
		{"nNP", false, false, true, false, false}, // parked
		{"nNH", false, false, false, true, false}, // at home
		{"I", true, false, false, false, false},   // parking == slewing
		{"NA", false, true, false, false, true},   // tracking, AltAz
	}
	for _, c := range cases {
		s := parseGU(c.gu)
		if s.Slewing != c.slew || s.Tracking != c.track || s.Parked != c.parked || s.AtHome != c.home || s.AltAz != c.altaz {
			t.Errorf("parseGU(%q) = %+v, want slew=%v track=%v park=%v home=%v altaz=%v",
				c.gu, s, c.slew, c.track, c.parked, c.home, c.altaz)
		}
	}
}

func TestStatusAndTracking(t *testing.T) {
	m, f := mount(map[string]string{":GU#": "N#"})
	if tr, err := m.Tracking(); err != nil || !tr {
		t.Errorf("Tracking = %v, %v; want true", tr, err)
	}
	if sl, _ := m.Slewing(); sl {
		t.Errorf("Slewing = true, want false")
	}
	// One :GU# for the burst (cache).
	if n := f.Count(":GU#"); n != 1 {
		t.Errorf(":GU# sent %d times, want 1", n)
	}

	m2, f2 := mount(map[string]string{":Te#": "0", ":Td#": "0"}) // '0' = success
	if err := m2.SetTracking(true); err != nil || f2.LastWrite() != ":Te#" {
		t.Errorf("SetTracking(true): %v wrote %q", err, f2.LastWrite())
	}
	if err := m2.SetTracking(false); err != nil || f2.LastWrite() != ":Td#" {
		t.Errorf("SetTracking(false): %v wrote %q", err, f2.LastWrite())
	}
}

func TestNumberedRateOverride(t *testing.T) {
	m, f := mount(nil)
	if err := m.SetRate(lx200.RateMax); err != nil || f.LastWrite() != ":R9#" {
		t.Errorf("SetRate(Max) wrote %q, want :R9#", f.LastWrite())
	}
	f.Reset()
	if err := m.MoveAxis(lx200.AxisSecondary, false, lx200.RateGuide); err != nil {
		t.Fatalf("MoveAxis: %v", err)
	}
	if w := f.Writes(); len(w) != 2 || w[0] != ":R2#" || w[1] != ":Ms#" {
		t.Errorf("MoveAxis wrote %v, want [:R2# :Ms#]", w)
	}
}

func TestParkAndSite(t *testing.T) {
	m, f := mount(map[string]string{
		":Sg+122:30:00#": "1",
		":St+37:30:00#":  "1",
		":hR#":           "1", // unpark acks '1'
	})
	if err := m.Park(); err != nil || f.LastWrite() != ":hP#" { // :hP# is blind
		t.Errorf("Park wrote %q, want :hP#", f.LastWrite())
	}
	if err := m.Unpark(); err != nil || f.LastWrite() != ":hR#" {
		t.Errorf("Unpark: %v wrote %q, want :hR#", err, f.LastWrite())
	}
	if err := m.SetSiteLongitude(-122.5); err != nil || f.LastWrite() != ":Sg+122:30:00#" {
		t.Errorf("SetSiteLongitude wrote %q, want :Sg+122:30:00#", f.LastWrite())
	}
	if err := m.SetSiteLatitude(37.5); err != nil || f.LastWrite() != ":St+37:30:00#" {
		t.Errorf("SetSiteLatitude wrote %q, want :St+37:30:00#", f.LastWrite())
	}
}

// TestSingleByteAcks: commands that reply a bare status byte must read+verify it
// (leaving it unread would desync the next :GU# poll). :hQ acks '1'; :RA/:RE '0'.
func TestSingleByteAcks(t *testing.T) {
	m, f := mount(map[string]string{
		":hQ#":        "1",
		":RA15.0410#": "0",
		":RE0.0000#":  "0",
	})
	if err := m.SetParkHere(); err != nil || f.LastWrite() != ":hQ#" {
		t.Errorf("SetParkHere: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetCustomTrackRate(15.0410, 0.0); err != nil {
		t.Errorf("SetCustomTrackRate: %v", err)
	}
	if got := f.LastWrite(); got != ":RE0.0000#" {
		t.Errorf("SetCustomTrackRate last wrote %q, want :RE0.0000#", got)
	}
}

// TestAckRejection: a non-success byte surfaces as an error (and is consumed).
func TestAckRejection(t *testing.T) {
	m, _ := mount(map[string]string{":hQ#": "0"}) // :hQ# success is '1'
	if err := m.SetParkHere(); err == nil {
		t.Errorf("SetParkHere with '0' reply: want rejection error")
	}
}
