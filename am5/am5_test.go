package am5

import (
	"testing"
	"time"

	"github.com/mikefsq/lx200"
)

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

func TestStatusIdleTracking(t *testing.T) {
	m, f := newMount(map[string]string{":GU#": "NG#", ":GAT#": "1#"})
	if sl, err := m.Slewing(); err != nil || sl {
		t.Errorf("Slewing = %v, %v; want false ('N' present)", sl, err)
	}
	if tr, _ := m.Tracking(); !tr {
		t.Errorf("Tracking = false, want true (:GAT# = 1)")
	}
	if mode, _ := m.MountMode(); mode != ModeEquatorial {
		t.Errorf("MountMode = %v, want Equatorial ('G')", mode)
	}
	// Burst coalesced: one :GU# + one :GAT#.
	gu, gat := 0, 0
	for _, w := range f.writes {
		switch w {
		case ":GU#":
			gu++
		case ":GAT#":
			gat++
		}
	}
	if gu != 1 || gat != 1 {
		t.Errorf("status round-trips: GU=%d GAT=%d, want 1/1", gu, gat)
	}
}

func TestStatusSlewingAndHome(t *testing.T) {
	m, _ := newMount(map[string]string{":GU#": "ZH#", ":GAT#": "0#"})
	if sl, _ := m.Slewing(); !sl {
		t.Errorf("Slewing = false, want true (no 'N')")
	}
	if h, _ := m.AtHome(); !h {
		t.Errorf("AtHome = false, want true ('H')")
	}
	if mode, _ := m.MountMode(); mode != ModeAltAz {
		t.Errorf("MountMode = %v, want AltAz ('Z')", mode)
	}
}

func TestTrackingCommands(t *testing.T) {
	m, f := newMount(map[string]string{":Te#": "1", ":Td#": "1"})
	if err := m.SetTracking(true); err != nil || f.writes[len(f.writes)-1] != ":Te#" {
		t.Errorf("SetTracking(true): %v wrote %q", err, f.writes[len(f.writes)-1])
	}
	if err := m.SetTracking(false); err != nil || f.writes[len(f.writes)-1] != ":Td#" {
		t.Errorf("SetTracking(false): %v wrote %q", err, f.writes[len(f.writes)-1])
	}
}

// TestNumberedRateOverride is the key regression: AM5 must emit numbered rates,
// not the core's letter presets, and MoveAxis must use AM5's SetRate.
func TestNumberedRateOverride(t *testing.T) {
	m, f := newMount(map[string]string{})

	if err := m.SetRate(lx200.RateMax); err != nil {
		t.Fatalf("SetRate: %v", err)
	}
	if got := f.writes[len(f.writes)-1]; got != ":R9#" {
		t.Errorf("SetRate(Max) wrote %q, want :R9# (not the core :RS#)", got)
	}

	f.writes = nil
	if err := m.MoveAxis(lx200.AxisPrimary, true, lx200.RateGuide); err != nil {
		t.Fatalf("MoveAxis: %v", err)
	}
	if len(f.writes) != 2 || f.writes[0] != ":R0#" || f.writes[1] != ":Me#" {
		t.Errorf("MoveAxis wrote %v, want [:R0# :Me#] (AM5 rate, not :RG#)", f.writes)
	}
}

func TestSiteLongitudeReversed(t *testing.T) {
	m, f := newMount(map[string]string{":Sg-123*30:00#": "1", ":St+45*30:00#": "1"})
	if err := m.SetSiteLongitude(123.5); err != nil { // East-positive -> Meade East-negative
		t.Errorf("SetSiteLongitude: %v", err)
	}
	if got := f.writes[len(f.writes)-1]; got != ":Sg-123*30:00#" {
		t.Errorf("SetSiteLongitude wrote %q, want :Sg-123*30:00#", got)
	}
	if err := m.SetSiteLatitude(45.5); err != nil {
		t.Errorf("SetSiteLatitude: %v", err)
	}
}

// TestSetUTC: INDI parity — send (negated) :SG offset + local :SC date, and
// never :SL or :Sev.
func TestSetUTC(t *testing.T) {
	cases := []struct {
		t              time.Time
		wantSG, wantSC string
	}{
		{time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC), ":SG+00:00#", ":SC06/02/26#"},
		{time.Date(2026, 6, 2, 12, 0, 0, 0, time.FixedZone("IST", 5*3600+30*60)), ":SG-05:30#", ":SC06/02/26#"},
		{time.Date(2026, 6, 2, 12, 0, 0, 0, time.FixedZone("EST", -5*3600)), ":SG+05:00#", ":SC06/02/26#"},
	}
	for _, c := range cases {
		m, f := newMount(map[string]string{c.wantSG: "1", c.wantSC: "1"})
		if err := m.SetUTC(c.t); err != nil {
			t.Fatalf("SetUTC(%v): %v", c.t, err)
		}
		if len(f.writes) != 2 || f.writes[0] != c.wantSG || f.writes[1] != c.wantSC {
			t.Errorf("SetUTC(%v) wrote %v, want [%s %s] (no :SL)", c.t, f.writes, c.wantSG, c.wantSC)
		}
	}
}

// TestElevationNoop: INDI never sends elevation, so SetSiteElevation writes nothing.
func TestElevationNoop(t *testing.T) {
	m, f := newMount(nil)
	if err := m.SetSiteElevation(1234); err != nil {
		t.Errorf("SetSiteElevation: %v", err)
	}
	if len(f.writes) != 0 {
		t.Errorf("SetSiteElevation sent %v; INDI sends nothing", f.writes)
	}
}

func TestSetHomeAndBuzzer(t *testing.T) {
	m, f := newMount(map[string]string{":SOa#": "1", ":GBu#": "2#"})
	if err := m.SetHome(); err != nil || f.writes[len(f.writes)-1] != ":SOa#" {
		t.Errorf("SetHome: %v wrote %q", err, f.writes[len(f.writes)-1])
	}
	if v, err := m.Buzzer(); err != nil || v != 2 {
		t.Errorf("Buzzer = %v, %v; want 2", v, err)
	}
}

func TestMeridianFlip(t *testing.T) {
	m, f := newMount(map[string]string{":GTa#": "11+05#", ":STa10-10#": "1"})
	mf, err := m.MeridianFlip()
	if err != nil || !mf.Enabled || !mf.TrackPast || mf.LimitDeg != 5 {
		t.Errorf("MeridianFlip = %+v, %v; want {Enabled:true TrackPast:true LimitDeg:5}", mf, err)
	}
	if err := m.SetMeridianFlip(MeridianFlip{Enabled: true, TrackPast: false, LimitDeg: -10}); err != nil {
		t.Errorf("SetMeridianFlip: %v", err)
	}
	if got := f.writes[len(f.writes)-1]; got != ":STa10-10#" {
		t.Errorf("SetMeridianFlip wrote %q, want :STa10-10#", got)
	}
}

func TestParkFlag(t *testing.T) {
	m, _ := newMount(map[string]string{":GU#": "NG#", ":GAT#": "1#"})
	if p, _ := m.AtPark(); p {
		t.Errorf("AtPark = true before park")
	}
	if err := m.Park(); err != nil { // :hC#
		t.Fatalf("Park: %v", err)
	}
	if p, _ := m.AtPark(); !p {
		t.Errorf("AtPark = false after Park")
	}
	if err := m.Unpark(); err != nil {
		t.Fatalf("Unpark: %v", err)
	}
	if p, _ := m.AtPark(); p {
		t.Errorf("AtPark = true after Unpark")
	}
}
