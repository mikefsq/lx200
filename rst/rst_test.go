package rst

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/mikefsq/lx200"
	"github.com/mikefsq/lx200/internal/lx200test"
	"github.com/mikefsq/lx200/serial"
)

func newMount(replies map[string]string) (*Mount, *lx200test.Fake) {
	// No reply is seeded for the slew commands: an accepted slew answers nothing, and only a
	// refusal speaks. A test that wants a refusal supplies it.
	// :AH# defaults to "no seek running". FindHome, Halt and Park all read it before acting, and
	// a test that is not about that guard should not have to seed it; one that is, overrides it.
	if replies == nil {
		replies = map[string]string{}
	}
	if _, ok := replies[":AH#"]; !ok {
		replies[":AH#"] = ":AH0#"
	}
	f := lx200test.New(replies)
	// Default to homed, since most tests exercise slews. TestRequireHomeGate covers the
	// un-homed path.
	return &Mount{Conn: lx200.New(f, 200*time.Millisecond), homeFound: true}, f
}

// A refused slew must surface as an error and must not latch slewing.
func TestSlewRefusalIsReportedAndDoesNotLatch(t *testing.T) {
	for _, c := range []struct {
		name  string
		reply map[string]string
		do    func(m *Mount) error
		want  string
	}{
		{"equatorial goto", map[string]string{":MS#": "MSZZ#"},
			func(m *Mount) error { return m.SlewToTarget() }, "MSZZ"},
		{"alt/az slew", map[string]string{":MA#": "MAZZ#"},
			func(m *Mount) error { return m.SlewToAltAz(180, 45) }, "MAZZ"},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, _ := newMount(c.reply)
			err := c.do(m)
			if err == nil {
				t.Fatal("a refused slew reported success")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want it to carry the on-wire fault %q", err, c.want)
			}
			if sl, _ := m.Slewing(); sl {
				t.Error("slewing latched after a refusal — nothing will ever clear it")
			}
		})
	}
}

// An accepted slew draws no reply at all, and must still latch so the move is tracked through
// to its completion token.
func TestSlewAcceptedLatches(t *testing.T) {
	m, f := newMount(nil)
	if err := m.SlewToTarget(); err != nil {
		t.Fatalf("SlewToTarget: %v", err)
	}
	if sl, _ := m.Slewing(); !sl {
		t.Error("an accepted slew did not latch slewing")
	}
	f.Push(":MM0#")
	time.Sleep(peekTTL)
	if sl, _ := m.Slewing(); sl {
		t.Error("the completion token did not clear slewing")
	}
}

// Slews are refused until the mount has homed.
func TestRequireHomeGate(t *testing.T) {
	m, _ := newMount(nil)
	m.homeFound = false
	if err := m.SlewToTarget(); err == nil {
		t.Error("SlewToTarget should be refused before homing")
	}
	if err := m.SlewToAltAz(180, 0); err == nil {
		t.Error("SlewToAltAz should be refused before homing")
	}
	if _, err := m.SyncToTarget(); err == nil {
		t.Error("SyncToTarget should be refused before homing")
	}
	m.homeFound = true
	if err := m.SlewToTarget(); err != nil {
		t.Errorf("SlewToTarget after home: %v", err)
	}
}

// Replies echo the command prefix, which puts the sign mid-string. It must still parse as
// negative.
func TestCoordPrefixAndSign(t *testing.T) {
	m, _ := newMount(map[string]string{
		":GR#": ":GR20:28:56.9#",
		":GD#": ":GD-52*13'21.9#",
	})
	if ra, err := m.RA(); err != nil || math.Abs(ra-(20+28.0/60+56.9/3600)) > 1e-6 {
		t.Errorf("RA = %v, %v", ra, err)
	}
	dec, err := m.Dec()
	want := -(52 + 13.0/60 + 21.9/3600)
	if err != nil || math.Abs(dec-want) > 1e-6 {
		t.Errorf("Dec = %v, %v; want %v (negative!)", dec, err, want)
	}
}

func TestTracking(t *testing.T) {
	m, _ := newMount(map[string]string{":AT#": ":AT1#"})
	if tr, err := m.Tracking(); err != nil || !tr {
		t.Errorf("Tracking = %v, %v; want true (:AT1#)", tr, err)
	}
	m2, _ := newMount(map[string]string{":AT#": ":AT0#"})
	if tr, _ := m2.Tracking(); tr {
		t.Errorf("Tracking = true, want false (:AT0#)")
	}
}

// The unsolicited completion-token model.
func TestSlewingViaToken(t *testing.T) {
	m, f := newMount(map[string]string{}) // :MS# has no immediate reply
	if err := m.SlewToTarget(); err != nil || f.LastWrite() != ":MS#" {
		t.Fatalf("SlewToTarget: %v wrote %q", err, f.LastWrite())
	}
	// No token yet, so still slewing.
	if sl, _ := m.Slewing(); !sl {
		t.Errorf("Slewing = false before token, want true")
	}
	// Mount pushes the completion token; next poll drains it.
	f.Push(":MM0#")
	time.Sleep(peekTTL) // allow the next peek (coalescing window)
	if sl, _ := m.Slewing(); sl {
		t.Errorf("Slewing = true after :MM0# token, want false")
	}
}

func TestSyncBuildsCk(t *testing.T) {
	m, f := newMount(map[string]string{
		":Sr20:30:00.0#":  "1",
		":Sd-52*00'00.0#": "1",
	})
	m.SetTargetRA(20.5)
	m.SetTargetDec(-52.0)
	if _, err := m.SyncToTarget(); err != nil {
		t.Fatalf("SyncToTarget: %v", err)
	}
	if got := f.LastWrite(); got != ":Ck307.500-52.000#" {
		t.Errorf("Sync wrote %q, want :Ck307.500-52.000#", got)
	}
}

func TestSiteFormat(t *testing.T) {
	m, f := newMount(nil) // :St/:Sg are blind on the RST — no reply to mock
	if err := m.SetSiteLatitude(37.5); err != nil || f.LastWrite() != ":St+37*30'00#" {
		t.Errorf("SetSiteLatitude: %v wrote %q", err, f.LastWrite())
	}
	if err := m.SetSiteLongitude(-122.5); err != nil || f.LastWrite() != ":Sg+122*30'00#" {
		t.Errorf("SetSiteLongitude(-122.5) wrote %q, want :Sg+122*30'00# (East-negative)", f.LastWrite())
	}
}

// The :CN save-alignment variant builds the same coordinates as a plain sync.
func TestAddAlignmentPoint(t *testing.T) {
	m, f := newMount(map[string]string{
		":Sr20:30:00.0#":  "1",
		":Sd-52*00'00.0#": "1",
	})
	m.SetTargetRA(20.5)
	m.SetTargetDec(-52.0)
	if err := m.AddAlignmentPoint(); err != nil {
		t.Fatalf("AddAlignmentPoint: %v", err)
	}
	if got := f.LastWrite(); got != ":CN307.500-52.000#" {
		t.Errorf("AddAlignmentPoint wrote %q, want :CN307.500-52.000#", got)
	}
}

// A pushed completion token sitting ahead of a coordinate reply must be consumed rather than
// parsed as coordinates.
func TestGetResyncPastStrayToken(t *testing.T) {
	m, f := newMount(map[string]string{
		":GR#": ":GR20:28:56.9#",
		":GD#": ":GD+00*00'00.0#",
		":GZ#": ":GZ270*00'00.0#",
		":GA#": ":GA+00*00'00.0#",
	})
	m.homeFound = false
	f.Push(":CHO#") // a home-completion token lands before our :GR reply
	ra, err := m.RA()
	if err != nil {
		t.Fatalf("RA past stray :CHO#: %v", err)
	}
	if want := 20 + 28.0/60 + 56.9/3600; math.Abs(ra-want) > 1e-6 {
		t.Errorf("RA = %v, want %v (the :CHO token should be skipped)", ra, want)
	}
	if !m.HomeFound() {
		t.Error("HomeFound = false; the consumed :CHO token should have latched it")
	}
	// The :CHO armed a deferred home capture, which a follow-up read performs, even though
	// the token was caught by the resync path rather than drainToken.
	if _, err := m.Dec(); err != nil {
		t.Fatalf("follow-up Dec: %v", err)
	}
	if _, _, _, _, ok := m.HomePosition(); !ok {
		t.Error("home position not captured after a follow-up read (deferred capture)")
	}
}

// The :Ct?# readback mapping.
func TestTrackMode(t *testing.T) {
	for _, c := range []struct {
		reply string
		want  TrackMode
	}{
		{":CT0#", TrackModeSidereal},
		{":CT1#", TrackModeSolar},
		{":CT2#", TrackModeLunar},
		{":CT3#", TrackModeCustom},
	} {
		m, _ := newMount(map[string]string{":Ct?#": c.reply})
		if got, err := m.TrackMode(); err != nil || got != c.want {
			t.Errorf("TrackMode(%s) = %v, %v; want %v", c.reply, got, err, c.want)
		}
	}
}

func TestPierSide(t *testing.T) {
	// decAxis - alignOff = 232.22 - 0 = 232.22 > 90 -> West.
	mW, _ := newMount(map[string]string{
		":CG3#": ":CG3+000.0000#",
		":CY#":  ":CY+232.22/-090.06#",
	})
	if ps, err := mW.PierSide(); err != nil || ps != lx200.PierWest {
		t.Errorf("PierSide = %v, %v; want West", ps, err)
	}
	// decAxis - alignOff = 45.0 - 0 = 45 <= 90 -> East.
	mE, _ := newMount(map[string]string{
		":CG3#": ":CG3+000.0000#",
		":CY#":  ":CY+045.00/-010.00#",
	})
	if ps, err := mE.PierSide(); err != nil || ps != lx200.PierEast {
		t.Errorf("PierSide = %v, %v; want East", ps, err)
	}
}

// The async stop must be the bare :Q#; the mount has no directional quit.
func TestPulseGuideStop(t *testing.T) {
	m, f := newMount(map[string]string{":CtU#": ":CTU#"}) // custom-track echo-ack
	if err := m.PulseGuide(lx200.North, 20); err != nil {
		t.Fatalf("PulseGuide: %v", err)
	}
	time.Sleep(60 * time.Millisecond) // let the stop goroutine fire
	if got := f.LastWrite(); got != ":Q#" {
		t.Errorf("PulseGuide stop wrote %q, want :Q#", got)
	}
	if m.IsPulseGuiding() {
		t.Errorf("IsPulseGuiding = true after stop, want false")
	}
}

// The per-axis stop collapses to the bare :Q#.
func TestStopAxis(t *testing.T) {
	m, f := newMount(nil)
	if err := m.StopAxis(lx200.AxisPrimary); err != nil || f.LastWrite() != ":Q#" {
		t.Errorf("StopAxis wrote %q, %v; want :Q#", f.LastWrite(), err)
	}
}

// Port selection in both enumeration regimes: the exact VID and PID match, and the macOS name
// fallback, which must fire only when no VID is reported.
func TestFindPort(t *testing.T) {
	cases := []struct {
		name  string
		ports []serial.PortInfo
		want  string
	}{
		{
			name: "exact FTDI VID/PID match",
			ports: []serial.PortInfo{
				{Name: "/dev/ttyUSB0", IsUSB: true, VID: "1234", PID: "5678"},
				{Name: "/dev/ttyUSB1", IsUSB: true, VID: "0403", PID: "6001"},
			},
			want: "/dev/ttyUSB1",
		},
		{
			name: "VID present but wrong PID is not claimed by the name fallback",
			ports: []serial.PortInfo{
				{Name: "/dev/cu.usbserial-A1", IsUSB: true, VID: "0403", PID: "6015"},
			},
			want: "",
		},
		{
			name: "macOS: no VID, FTDI VCP name matches",
			ports: []serial.PortInfo{
				{Name: "/dev/cu.usbserial-1410", IsUSB: true},
			},
			want: "/dev/cu.usbserial-1410",
		},
		{
			name: "macOS: no VID, non-FTDI name (usbmodem) does not match",
			ports: []serial.PortInfo{
				{Name: "/dev/cu.usbmodem1411", IsUSB: true},
			},
			want: "",
		},
		{
			name: "exact match wins over a name-only candidate",
			ports: []serial.PortInfo{
				{Name: "/dev/cu.usbserial-novid", IsUSB: true},
				{Name: "/dev/cu.usbserial-rst", IsUSB: true, VID: "0403", PID: "6001"},
			},
			want: "/dev/cu.usbserial-rst",
		},
		{
			name:  "empty list",
			ports: nil,
			want:  "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := findPort(c.ports); got != c.want {
				t.Errorf("findPort = %q, want %q", got, c.want)
			}
		})
	}
}

// candidatePorts must return every plausible port, not just the best one.
func TestCandidatePorts(t *testing.T) {
	cases := []struct {
		name  string
		ports []serial.PortInfo
		want  []string
	}{
		{
			name: "two identical FTDI bridges: both are candidates, in enumeration order",
			ports: []serial.PortInfo{
				{Name: "/dev/ttyUSB0", IsUSB: true, VID: "0403", PID: "6001", SerialNumber: "AG0JWD3W"},
				{Name: "/dev/ttyUSB1", IsUSB: true, VID: "0403", PID: "6015", SerialNumber: "D30B0DP6"},
				{Name: "/dev/ttyUSB2", IsUSB: true, VID: "0403", PID: "6001", SerialNumber: "A10KLC4K"},
			},
			want: []string{"/dev/ttyUSB0", "/dev/ttyUSB2"},
		},
		{
			name: "VID matches sort ahead of name-only fallbacks",
			ports: []serial.PortInfo{
				{Name: "/dev/cu.usbserial-novid", IsUSB: true},
				{Name: "/dev/cu.usbserial-rst", IsUSB: true, VID: "0403", PID: "6001"},
			},
			want: []string{"/dev/cu.usbserial-rst", "/dev/cu.usbserial-novid"},
		},
		{
			name:  "nothing plausible",
			ports: []serial.PortInfo{{Name: "/dev/ttyS0"}},
			want:  nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := candidatePorts(c.ports)
			if len(got) != len(c.want) {
				t.Fatalf("candidatePorts = %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("candidatePorts = %v, want %v", got, c.want)
				}
			}
		})
	}
}

func TestTrackRatesAndPark(t *testing.T) {
	// Park is an equatorial goto to HA +6h and Dec 89*59', which pins the RA axis at
	// mechanical zero. AtPark reads the axes; AlpacaAtPark adds the "and stopped" guarantee.
	m, f := newMount(map[string]string{
		":CtR#":           ":CTR#",        // echoed track command
		":CtL#":           ":CTL#",        // Park -> SetTracking(false)
		":GS#":            ":GS16:00:00#", // LST -> target RA = LST - 6h = 10h
		":Sr10:00:00.0#":  "1",
		":Sd+89*59'00.0#": "1",
		":CY#":            awayAxes, // away from the park, so Park slews rather than latching
	})
	if err := m.TrackSidereal(); err != nil || f.LastWrite() != ":CtR#" {
		t.Errorf("TrackSidereal wrote %q, want :CtR#", f.LastWrite())
	}
	if err := m.Park(); err != nil || f.LastWrite() != ":MS#" {
		t.Errorf("Park: %v final write %q, want :MS#", err, f.LastWrite())
	}
	// AtPark answers about the position and does not wait for the token. Not-yet-parked
	// during the slew is AlpacaAtPark's guarantee.
	if p, _ := m.AlpacaAtPark(); p {
		t.Error("AlpacaAtPark = true before the completion token")
	}
	f.SetReply(":CY#", parkedAxes) // the slew has carried the axes onto the polar axis
	f.Push(":MM0#")
	time.Sleep(peekTTL)
	if sl, _ := m.Slewing(); sl { // drains the token, latching parked
		t.Error("still slewing after :MM0#")
	}
	if p, err := m.AlpacaAtPark(); err != nil || !p {
		t.Errorf("AlpacaAtPark = %v, %v; want true after park completion", p, err)
	}
	if err := m.Unpark(); err != nil {
		t.Fatalf("Unpark: %v", err)
	}
	// Unpark does not move the mount, so the axes still say parked; the Alpaca guard is what
	// honours the state change.
	if p, _ := m.AlpacaAtPark(); p {
		t.Error("AlpacaAtPark = true after Unpark; want false")
	}
	if p, _ := m.AtPark(); !p {
		t.Error("AtPark = false after Unpark; the tube has not moved, so the axes still say parked")
	}
}

// :AH# indicates an active home seek and clears when it finishes.
func TestHomingReportsTheBusyGuardNotAtHome(t *testing.T) {
	m, f := newMount(map[string]string{":AH#": ":AH1#"})
	if busy, err := m.Homing(); err != nil || !busy {
		t.Errorf("Homing = %v, %v; want true", busy, err)
	}
	if f.LastWrite() != ":AH#" {
		t.Errorf("Homing wrote %q, want :AH#", f.LastWrite())
	}

	m, _ = newMount(map[string]string{":AH#": ":AH0#"})
	if busy, err := m.Homing(); err != nil || busy {
		t.Errorf("Homing = %v, %v; want false", busy, err)
	}
}

// AtHome is gated on HomeFound, then decided by the mechanical angles, and must not read :AH#.
func TestAtHomeIsHomeFoundThenAxisAngles(t *testing.T) {
	m, f := newMount(map[string]string{":CY#": ":CY+000.00/-000.00#"})
	if at, err := m.AtHome(); err != nil || !at {
		t.Errorf("AtHome = %v, %v; want true — homed and both axes at 0", at, err)
	}
	for _, w := range f.Writes() {
		if w == ":AH#" {
			t.Error("AtHome queried :AH#, which is the homing busy guard, not an at-home flag")
		}
		if w == ":GZ#" || w == ":GA#" {
			t.Errorf("AtHome read %s; it must use the mechanical axis angles (:CY#), not the "+
				"derived horizon coordinates — a stale coordinate model reports Az 270/Alt 0 "+
				"with the RA axis 200 degrees away", w)
		}
	}
}

// Without a completed seek the mount has no mechanical reference, so the angles mean nothing.
func TestAtHomeRequiresHomeFound(t *testing.T) {
	m, _ := newMount(map[string]string{":CY#": ":CY+000.00/-000.00#"})
	m.mu.Lock()
	m.homeFound = false
	m.mu.Unlock()
	if at, _ := m.AtHome(); at {
		t.Error("AtHome = true without HomeFound")
	}
}

// Homed, but the axes are elsewhere. Each case moves one axis out of tolerance, so a test that
// checked only the other would fail here.
func TestAtHomeFalseAwayFromHome(t *testing.T) {
	for _, c := range []struct{ name, cy string }{
		{"RA axis 200 degrees out", ":CY+000.00/-200.06#"},
		{"Dec axis folded up", ":CY+089.49/-000.00#"},
		{"both axes away", ":CY+045.00/-010.00#"},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, _ := newMount(map[string]string{":CY#": c.cy})
			if at, _ := m.AtHome(); at {
				t.Errorf("AtHome = true at %s", c.cy)
			}
		})
	}
}

// The angles wrap, so the comparison goes the short way round.
func TestAtHomeToleranceWrapsAtZero(t *testing.T) {
	m, _ := newMount(map[string]string{":CY#": ":CY+359.90/-359.50#"})
	if at, err := m.AtHome(); err != nil || !at {
		t.Errorf("AtHome = %v, %v; want true — 359.9 and 359.5 are within tolerance of 0", at, err)
	}
}

// The Dec axis is held tighter than the RA axis, and each bound is checked from both sides.
func TestAtHomeAxisTolerances(t *testing.T) {
	for _, c := range []struct {
		name string
		cy   string
		want bool
	}{
		{"dec just inside", ":CY+000.90/-000.00#", true},
		{"dec just outside", ":CY+001.10/-000.00#", false},
		{"ra just inside", ":CY+000.00/-004.90#", true},
		{"ra just outside", ":CY+000.00/-005.10#", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, _ := newMount(map[string]string{":CY#": c.cy})
			at, err := m.AtHome()
			if err != nil || at != c.want {
				t.Errorf("AtHome at %s = %v, %v; want %v", c.cy, at, err, c.want)
			}
		})
	}
}

// The filter is what lets a caller stop reopening a neighbour's port.
func TestCandidatesFilter(t *testing.T) {
	ports := []serial.PortInfo{
		{Name: "/dev/ttyUSB0", IsUSB: true, VID: "0403", PID: "6001", SerialNumber: "AG0JWD3W"},
		{Name: "/dev/ttyUSB1", IsUSB: true, VID: "0403", PID: "6015", SerialNumber: "D30B0DP6"},
		{Name: "/dev/ttyUSB2", IsUSB: true, VID: "0403", PID: "6001", SerialNumber: "A10KLC4K"},
	}
	names := func(ps []serial.PortInfo) []string {
		var out []string
		for _, p := range ps {
			out = append(out, p.Name)
		}
		return out
	}
	join := func(ss []string) string { return strings.Join(ss, ",") }

	if got := join(names(candidates(ports, Filter{}))); got != "/dev/ttyUSB0,/dev/ttyUSB2" {
		t.Errorf("no filter = %s, want both 6001 bridges", got)
	}
	// A pin selects exactly one, and matches case-insensitively: the value round-trips through
	// a config file an operator may well have typed by hand.
	if got := join(names(candidates(ports, Filter{Serial: "a10klc4k"}))); got != "/dev/ttyUSB2" {
		t.Errorf("pinned = %s, want only ttyUSB2", got)
	}
	if got := join(names(candidates(ports, Filter{Serial: " A10KLC4K "}))); got != "/dev/ttyUSB2" {
		t.Errorf("pinned with whitespace = %s, want only ttyUSB2", got)
	}
	if got := candidates(ports, Filter{Serial: "NOSUCH"}); len(got) != 0 {
		t.Errorf("pinned to an absent serial = %v, want none", names(got))
	}
}

// Only matching USB serials are probed when a pin is set. Silent ports remain
// eligible on subsequent scans because the mount may still be starting.
func TestCandidatesKeepsUnidentifiablePorts(t *testing.T) {
	ports := []serial.PortInfo{{Name: "/dev/cu.usbserial-1410", IsUSB: true}}
	if got := candidates(ports, Filter{}); len(got) != 1 {
		t.Errorf("port with no serial = %v, want it kept", got)
	}
	// It has no serial to match, so a pin cannot select it either.
	if got := candidates(ports, Filter{Serial: "A10KLC4K"}); len(got) != 0 {
		t.Errorf("port with no serial under a pin = %v, want none", got)
	}
}

// A refused alignment must surface as an error.
func TestSyncRefusalIsReported(t *testing.T) {
	m, f := newMount(map[string]string{
		":Sr00:00:00.0#":     "1",
		":Sd+00*00'00.0#":    "1",
		":Ck000.000+00.000#": ":CMF#",
	})
	m.SetTargetRA(0)
	m.SetTargetDec(0)
	s, err := m.SyncToTarget()
	if err == nil {
		t.Fatalf("SyncToTarget returned %q, nil; want an error for a refused sync", s)
	}
	if !strings.Contains(err.Error(), "CMF") {
		t.Errorf("error %q does not carry the mount's verdict", err)
	}
	if f.LastWrite() != ":Ck000.000+00.000#" {
		t.Errorf("wrote %q", f.LastWrite())
	}
}

// An accepted alignment returns the mount's confirmation text, prefix stripped.
func TestSyncAcceptedReturnsConfirmation(t *testing.T) {
	m, _ := newMount(map[string]string{
		":Sr00:00:00.0#":     "1",
		":Sd+00*00'00.0#":    "1",
		":Ck000.000+00.000#": ":CMSynced#",
	})
	m.SetTargetRA(0)
	m.SetTargetDec(0)
	got, err := m.SyncToTarget()
	if err != nil || got != "Synced" {
		t.Errorf("SyncToTarget = %q, %v; want \"Synced\", nil", got, err)
	}
}

// Silence is not a failure. The verdict can arrive after the window and applyToken routes it
// then, so treating a quiet mount as an error would make every slow sync look refused.
func TestSyncSilenceIsNotAnError(t *testing.T) {
	// :Sr/:Sd ack; :Ck deliberately has no reply seeded.
	m, _ := newMount(map[string]string{
		":Sr00:00:00.0#":  "1",
		":Sd+00*00'00.0#": "1",
	})
	m.SetTargetRA(0)
	m.SetTargetDec(0)
	if got, err := m.SyncToTarget(); err != nil || got != "" {
		t.Errorf("SyncToTarget = %q, %v; want \"\", nil on silence", got, err)
	}
}

// A CMF verdict arriving later as a stray token must still register as a fault.
func TestLateAlignmentRefusalRecordsFault(t *testing.T) {
	m, _ := newMount(nil)
	m.applyToken(":CMF")
	if got := m.Fault(); !strings.Contains(got, "CMF") {
		t.Errorf("Fault = %q; want the CMF verdict recorded", got)
	}
}

// An accepted CM frame is not a fault.
func TestLateAlignmentSuccessIsNotAFault(t *testing.T) {
	m, _ := newMount(nil)
	m.applyToken(":CMSynced")
	if got := m.Fault(); got != "" {
		t.Errorf("Fault = %q; want empty after an accepted sync", got)
	}
}

// SerialNumber identifies the mount, not the USB bridge. The values are real readings, so the
// fixture doubles as a record of the wire format.
func TestSerialNumber(t *testing.T) {
	m, f := newMount(map[string]string{":AS#": ":AS350021#"})
	got, err := m.SerialNumber()
	if err != nil || got != "350021" {
		t.Errorf("SerialNumber = %q, %v; want \"350021\"", got, err)
	}
	if f.LastWrite() != ":AS#" {
		t.Errorf("wrote %q, want :AS#", f.LastWrite())
	}
}

// :AG# and :AP# return two fields joined by '*'.
func TestGearAndWorm(t *testing.T) {
	m, _ := newMount(map[string]string{":AG#": ":AG017842176*008921088#"})
	ra, dec, err := m.GearRatio()
	if err != nil || ra != 17842176 || dec != 8921088 {
		t.Errorf("GearRatio = %d, %d, %v; want 17842176, 8921088", ra, dec, err)
	}

	m, _ = newMount(map[string]string{":AP#": ":AP0100*0100#"})
	if ra, dec, err = m.WormCount(); err != nil || ra != 100 || dec != 100 {
		t.Errorf("WormCount = %d, %d, %v; want 100, 100", ra, dec, err)
	}
}

// A reply that does not split on '*' must error rather than yield zeros; a zero gear ratio
// would be a catastrophic value to hand a rate calculation.
func TestGearRatioRejectsMalformedReply(t *testing.T) {
	m, _ := newMount(map[string]string{":AG#": ":AG017842176#"})
	if ra, dec, err := m.GearRatio(); err == nil {
		t.Errorf("GearRatio = %d, %d, nil; want an error for a reply with no '*'", ra, dec)
	}
}

// The horizontal target setters are blind, so the readback is the only confirmation.
func TestTargetReadback(t *testing.T) {
	m, _ := newMount(map[string]string{
		":Gr#": ":Gr05:34:30.0#",
		":Gd#": ":Gd+22*00'59.9#",
	})
	if ra, err := m.TargetRA(); err != nil || math.Abs(ra-5.575) > 1e-3 {
		t.Errorf("TargetRA = %v, %v; want ~5.575 (M1)", ra, err)
	}
	if dec, err := m.TargetDec(); err != nil || math.Abs(dec-22.0166) > 1e-3 {
		t.Errorf("TargetDec = %v, %v; want ~22.017 (M1)", dec, err)
	}

	m, _ = newMount(map[string]string{
		":Gz#": ":Gz090*00'00.0#",
		":Ga#": ":Ga+00*00'00.0#",
	})
	az, alt, err := m.TargetAltAz()
	if err != nil || math.Abs(az-90) > 1e-6 || math.Abs(alt) > 1e-6 {
		t.Errorf("TargetAltAz = %v, %v, %v; want 90, 0", az, alt, err)
	}
}

// The six limit registers, with the values read from real hardware.
func TestSlewLimits(t *testing.T) {
	m, _ := newMount(map[string]string{
		":CA#": ":CA085#", ":CB#": ":CB020#", ":CC#": ":CC-022.399#",
		":CD#": ":CD150#", ":CE#": ":CE020#", ":CF#": ":CF-006.900#",
	})
	got, err := m.SlewLimits()
	if err != nil {
		t.Fatalf("SlewLimits: %v", err)
	}
	want := [6]float64{85, 20, -22.399, 150, 20, -6.9}
	for i := range want {
		if math.Abs(got[i]-want[i]) > 1e-6 {
			t.Errorf("SlewLimits[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

// AtPark asks the mount with no latch involved, so reconnecting to a parked mount, or a park
// done from the handset, still reads true.
func TestAtParkReadsTheAxesWithNoLatch(t *testing.T) {
	m, f := newMount(map[string]string{":CY#": parkedAxes})
	if at, err := m.AtPark(); err != nil || !at {
		t.Errorf("AtPark = %v, %v; want true — the axes are on the polar axis", at, err)
	}
	for _, w := range f.Writes() {
		if w == ":GZ#" || w == ":GA#" || w == ":Gt#" {
			t.Errorf("AtPark read %s; it must use the mechanical axis angles (:CY#) — horizon "+
				"coordinates cannot tell the intended stow from a pole stow in the wrong rotation", w)
		}
	}
}

// Pointing elsewhere reads as not parked.
func TestAtParkFalseAwayFromThePolarAxis(t *testing.T) {
	m, _ := newMount(map[string]string{":CY#": ":CY+045.00/-010.00#"})
	if at, err := m.AtPark(); err != nil || at {
		t.Errorf("AtPark = %v, %v; want false", at, err)
	}
}

// The RA axis reads a signed angle that wraps, so 359.95 and -0.05 are the same place.
func TestAtParkHandlesRAAxisWrap(t *testing.T) {
	m, _ := newMount(map[string]string{":CY#": ":CY+089.98/+359.95#"})
	if at, err := m.AtPark(); err != nil || !at {
		t.Errorf("AtPark = %v, %v; want true — 359.95 is 0.05 from 0", at, err)
	}
}

// Each bound from both sides. The Dec band is 90 +0/-0.1: the axis cannot fold past the polar
// axis, so 90.00 is in and anything beyond it is out.
func TestAtParkAxisBounds(t *testing.T) {
	for _, c := range []struct {
		name string
		cy   string
		want bool
	}{
		{"dec at the low bound", ":CY+089.91/-000.00#", true},
		{"dec below the low bound", ":CY+089.85/-000.00#", false},
		{"dec at exactly 90", ":CY+090.00/-000.00#", true},
		{"dec past 90", ":CY+090.05/-000.00#", false},
		{"ra just inside", ":CY+089.98/-000.09#", true},
		{"ra just outside", ":CY+089.98/-000.15#", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, _ := newMount(map[string]string{":CY#": c.cy})
			at, err := m.AtPark()
			if err != nil || at != c.want {
				t.Errorf("AtPark at %s = %v, %v; want %v", c.cy, at, err, c.want)
			}
		})
	}
}

// In the southern hemisphere the pole is at Az 180, Alt = |latitude|.
func TestPolePositionSouthernHemisphere(t *testing.T) {
	m, _ := newMount(map[string]string{":Gt#": ":Gt-33*52'00.0#"})
	az, alt, err := m.polePosition()
	if err != nil || math.Abs(az-180) > 1e-6 || math.Abs(alt-33.8666) > 1e-3 {
		t.Errorf("polePosition = %v, %v, %v; want 180, ~33.867", az, alt, err)
	}
}

// :AU# and :AR# each write the configuration EEPROM unconditionally, and this driver reconnects
// every 3 seconds, so sending them on every connect would cycle a flash cell thousands of times
// a day. A mount already in the Rainbow dialect must be left alone.
func TestSelectDialectSkipsTheWriteWhenAlreadyInRainbowMode(t *testing.T) {
	m, f := newMount(map[string]string{":GR#": ":GR01:02:03.4#"})
	m.selectDialect()
	for _, w := range f.Writes() {
		if w == ":AU#" || w == ":AR#" {
			t.Errorf("selectDialect wrote %q to a mount already in Rainbow mode", w)
		}
	}
}

// A mount answering without the echoed prefix is in the plain LX200 dialect and does need
// configuring.
func TestSelectDialectWritesWhenTheEchoIsOff(t *testing.T) {
	m, f := newMount(map[string]string{":GR#": "01:02:03.4#"}) // bare reply: echo is off
	m.selectDialect()
	var sawAU, sawAR bool
	for _, w := range f.Writes() {
		sawAU = sawAU || w == ":AU#"
		sawAR = sawAR || w == ":AR#"
	}
	if !sawAU || !sawAR {
		t.Errorf("selectDialect wrote %v; want :AU# and :AR# when the echo is off", f.Writes())
	}
}

// Exceeding the reply skip budget must drain and retry without returning a stray frame.
func TestGetRetriesPastExcessStrayTokens(t *testing.T) {
	m, f := newMount(map[string]string{":GR#": ":GR20:28:56.9#"})
	// Five stray tokens ahead of the reply, past the skip budget of a single attempt.
	f.Push(":MM0#:MM0#:MM0#:MM0#:MM0#")
	ra, err := m.RA()
	if err != nil {
		t.Fatalf("RA past excess stray tokens: %v", err)
	}
	if want := 20 + 28.0/60 + 56.9/3600; math.Abs(ra-want) > 1e-6 {
		t.Errorf("RA = %v, want %v; the retry should recover the real reply", ra, want)
	}
}

// When the reply never comes, the read must return an error rather than a wrong value.
func TestGetErrorsRatherThanReturningGarbage(t *testing.T) {
	m, f := newMount(nil) // no :GR# reply scripted
	f.Push(":MM0#:MM0#:MM0#:MM0#:MM0#:MM0#:MM0#:MM0#")
	if _, err := m.RA(); err == nil {
		t.Error("RA returned nil error with no real reply; it must not hand back a stray token")
	}
}

// Park commands hour angle +6h to pin the RA axis at zero near the pole.
func TestParkCommandsHourAnglePlusSixNotThePolePosition(t *testing.T) {
	for _, c := range []struct{ name, lst, wantRA string }{
		{"midday LST", ":GS16:00:00#", ":Sr10:00:00.0#"},
		{"wraps past 0h", ":GS03:00:00#", ":Sr21:00:00.0#"}, // 3 - 6 = -3 -> 21
		{"exactly 6h", ":GS06:00:00#", ":Sr00:00:00.0#"},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, f := newMount(map[string]string{
				":CtL#":           ":CTL#",
				":GS#":            c.lst,
				c.wantRA:          "1",
				":Sd+89*59'00.0#": "1",
			})
			if err := m.Park(); err != nil {
				t.Fatalf("Park: %v", err)
			}
			var sawRA, sawDec bool
			for _, w := range f.Writes() {
				switch w {
				case c.wantRA:
					sawRA = true
				case ":Sd+89*59'00.0#":
					sawDec = true
				case ":Sz000*00'00.0#", ":MA#":
					t.Errorf("Park wrote %q — that is the alt/az goto, which cannot pin the RA axis", w)
				}
			}
			if !sawRA {
				t.Errorf("Park did not write %q; writes: %v", c.wantRA, f.Writes())
			}
			if !sawDec {
				t.Errorf("Park did not write the Dec target; writes: %v", f.Writes())
			}
		})
	}
}

// Dec 90 exactly would reintroduce the singularity the hour angle exists to avoid, so this pins
// the constant against being rounded up.
func TestParkDecStaysOffTheSingularity(t *testing.T) {
	if parkDec >= 90 {
		t.Fatalf("parkDec = %v; must be < 90, or the RA target is degenerate again", parkDec)
	}
	if 90-parkDec > 0.5 {
		t.Errorf("parkDec = %v; more than 0.5 deg from the pole is a worse stow than it needs to be", parkDec)
	}
	// It also has to survive the mount's own arithmetic, which is coarser than the frame's.
	// A target of 89*59'59.9 is well formed, but the mount rounds it to a flat Dec 90 and the
	// RA axis landed at -30.09. One arcminute is the measured floor.
	if 90-parkDec < 1.0/60 {
		t.Errorf("parkDec = %v is inside one arcminute of the pole; the mount rounds that to "+
			"Dec 90 and the RA axis is no longer pinned (measured: RA axis -30.09)", parkDec)
	}
}

// Park is a slew, and every slew on this mount is gated on a completed home seek.
func TestParkRequiresHomeFound(t *testing.T) {
	m, f := newMount(map[string]string{":CtL#": ":CTL#", ":GS#": ":GS16:00:00#"})
	m.mu.Lock()
	m.homeFound = false
	m.mu.Unlock()
	if err := m.Park(); err == nil {
		t.Error("Park succeeded without HomeFound")
	}
	for _, w := range f.Writes() {
		if w == ":MS#" {
			t.Error("Park issued the goto despite the mount not being homed")
		}
	}
}

// Round sexagesimal values before splitting fields to preserve minute boundaries.
func TestSexagesimalRoundsBeforeSplitting(t *testing.T) {
	for _, c := range []struct {
		name string
		deg  float64
		want string
	}{
		{"the case that exposed it", 89 + 59.0/60.0, "+89*59'00.0"},
		{"parkDec", parkDec, "+89*59'00.0"},
		{"exact arcminute from division", 37 + 57.0/60.0, "+37*57'00.0"},
		{"carries into the next minute", 1.0 - 0.05/3600, "+01*00'00.0"},
		{"whole degree", 52, "+52*00'00.0"},
		{"negative keeps magnitude", -(89 + 59.0/60.0), "-89*59'00.0"},
		{"fractional arcsecond survives", 12 + 34.0/60 + 56.7/3600, "+12*34'56.7"},
	} {
		t.Run(c.name, func(t *testing.T) {
			sign, d, m, s := dmsParts(c.deg)
			got := fmt.Sprintf("%c%02d*%02d'%04.1f", sign, d, m, s)
			if got != c.want {
				t.Errorf("dmsParts(%v) = %q, want %q", c.deg, got, c.want)
			}
		})
	}
	for _, c := range []struct {
		name  string
		hours float64
		want  string
	}{
		{"exact minute from division", 10 + 30.0/60, "10:30:00.0"},
		{"carries into the next minute", 5 - 0.04/3600, "05:00:00.0"},
		{"wraps rather than printing 24h", 24 - 0.04/3600, "00:00:00.0"},
		{"fractional second survives", 20 + 28.0/60 + 56.9/3600, "20:28:56.9"},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, m, s := hms(c.hours)
			got := fmt.Sprintf("%02d:%02d:%04.1f", h, m, s)
			if got != c.want {
				t.Errorf("hms(%v) = %q, want %q", c.hours, got, c.want)
			}
		})
	}
}

// A park must leave the mount stationary, and sending :CtL# first is not enough. An equatorial
// goto starts tracking when it arrives, verified on hardware, so the pre-slew stop is undone by
// the slew itself and the mount drives off the park at the sidereal rate.
func TestParkStopsTrackingAfterTheSlewArrives(t *testing.T) {
	m, f := newMount(map[string]string{
		":CtL#":           ":CTL#",
		":GS#":            ":GS16:00:00#",
		":Sr10:00:00.0#":  "1",
		":Sd+89*59'00.0#": "1",
		":CY#":            awayAxes, // away from the park, so Park slews
	})
	if err := m.Park(); err != nil {
		t.Fatalf("Park: %v", err)
	}
	f.SetReply(":CY#", parkedAxes) // arrived
	// One :CtL# so far, the pre-slew one. The mount is still moving and the goto has not yet
	// turned tracking back on.
	if n := countWrites(f, ":CtL#"); n != 1 {
		t.Errorf("%d :CtL# during the slew, want 1 (the pre-slew stop)", n)
	}

	f.Push(":MM0#") // the park arrives
	time.Sleep(peekTTL)
	if sl, _ := m.Slewing(); sl {
		t.Fatal("still slewing after :MM0#")
	}
	if n := countWrites(f, ":CtL#"); n != 2 {
		t.Errorf("%d :CtL# after the park arrived, want 2 — the goto turned tracking back on "+
			"and nothing stopped it, so the mount drives off the park at the sidereal rate", n)
	}

	// Armed once, not once per poll; a parked mount must not be re-sent :CtL# forever.
	if _, err := m.Slewing(); err != nil {
		t.Fatalf("Slewing: %v", err)
	}
	if _, err := m.AtPark(); err != nil {
		t.Fatalf("AtPark: %v", err)
	}
	if n := countWrites(f, ":CtL#"); n != 2 {
		t.Errorf("%d :CtL# after further polling, want 2 — the deferred stop must fire once", n)
	}
}

// A new slew cancels a park that has not landed yet, including the tracking-off that would have
// followed it, since the mount is going somewhere else and must be left tracking.
func TestANewSlewCancelsAPendingParkStop(t *testing.T) {
	m, f := newMount(map[string]string{
		":CtL#":           ":CTL#",
		":GS#":            ":GS16:00:00#",
		":Sr10:00:00.0#":  "1",
		":Sd+89*59'00.0#": "1",
		":Sr20:00:00.0#":  "1",
		":Sd+30*00'00.0#": "1",
		":CY#":            awayAxes, // away from the park, so Park slews
	})
	if err := m.Park(); err != nil {
		t.Fatalf("Park: %v", err)
	}
	if _, err := m.SetTargetRA(20); err != nil {
		t.Fatalf("SetTargetRA: %v", err)
	}
	if _, err := m.SetTargetDec(30); err != nil {
		t.Fatalf("SetTargetDec: %v", err)
	}
	if err := m.SlewToTarget(); err != nil { // supersedes the park
		t.Fatalf("SlewToTarget: %v", err)
	}
	before := countWrites(f, ":CtL#")
	f.Push(":MM0#") // this token belongs to the NEW slew
	time.Sleep(peekTTL)
	if _, err := m.Slewing(); err != nil {
		t.Fatalf("Slewing: %v", err)
	}
	if n := countWrites(f, ":CtL#"); n != before {
		t.Errorf("%d :CtL# after the superseding slew, want %d — the cancelled park must not "+
			"stop tracking on a goto the caller wants tracked", n, before)
	}
}

// countWrites counts how many times cmd was written.
func countWrites(f *lx200test.Fake, cmd string) int {
	n := 0
	for _, w := range f.Writes() {
		if w == cmd {
			n++
		}
	}
	return n
}

// A mount already parked must stop without a slew: a zero-distance goto
// may never produce a completion token.
func TestParkAtTheParkPositionSkipsTheGoto(t *testing.T) {
	m, f := newMount(map[string]string{
		":CtL#": ":CTL#",
		":CY#":  parkedAxes,
	})
	m.mu.Lock()
	m.unparked = true // as an Unpark leaves it: at the park position, not in the parked state
	m.mu.Unlock()

	if err := m.Park(); err != nil {
		t.Fatalf("Park: %v", err)
	}
	for _, w := range f.Writes() {
		switch w {
		case ":MS#", ":GS#":
			t.Errorf("Park wrote %q from the park position; a zero-distance goto never completes", w)
		}
	}
	if n := countWrites(f, ":CtL#"); n != 1 {
		t.Errorf("%d :CtL# want 1 — tracking must still be stopped, just without a slew", n)
	}
	if sl, _ := m.Slewing(); sl {
		t.Error("Slewing latched with no slew in flight")
	}
	if at, err := m.AlpacaAtPark(); err != nil || !at {
		t.Errorf("AlpacaAtPark = %v, %v; want true — the state is what Park changed", at, err)
	}
}

// Away from the park it still slews, so the skip is a special case and not the rule.
func TestParkAwayFromTheParkPositionStillSlews(t *testing.T) {
	m, f := newMount(map[string]string{
		":CtL#":           ":CTL#",
		":GS#":            ":GS16:00:00#",
		":Sr10:00:00.0#":  "1",
		":Sd+89*59'00.0#": "1",
		":CY#":            awayAxes,
	})
	if err := m.Park(); err != nil {
		t.Fatalf("Park: %v", err)
	}
	if f.LastWrite() != ":MS#" {
		t.Errorf("Park final write %q, want :MS#", f.LastWrite())
	}
}

// A home seek blocks commands that can leave the firmware busy until power-cycled.
func TestCommandsThatCanWedgeAHomeSeekAreRefused(t *testing.T) {
	for _, c := range []struct {
		name string
		do   func(*Mount) error
	}{
		{"FindHome", func(m *Mount) error { return m.FindHome() }},
		{"Halt", func(m *Mount) error { return m.Halt() }},
		{"Park", func(m *Mount) error { return m.Park() }},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, f := newMount(map[string]string{
				":AH#":  ":AH1#", // a seek is running
				":CtL#": ":CTL#",
				":CY#":  awayAxes,
			})
			err := c.do(m)
			if !errors.Is(err, ErrHoming) {
				t.Fatalf("%s = %v; want ErrHoming", c.name, err)
			}
			for _, w := range f.Writes() {
				switch w {
				case ":Ch#", ":Q#", ":MS#":
					t.Errorf("%s wrote %q while a seek was running", c.name, w)
				}
			}
		})
	}
}

// :Ch# is blind, so a re-entrant one the firmware discards is indistinguishable from one it
// accepted. Latching Slewing on it leaves the driver waiting out its full timeout for a token
// that is never coming, which is what a stuck busy flag looked like on hardware.
func TestFindHomeRefusedWhileBusyDoesNotLatchSlewing(t *testing.T) {
	m, _ := newMount(map[string]string{":AH#": ":AH1#", ":CtL#": ":CTL#"})
	if err := m.FindHome(); !errors.Is(err, ErrHoming) {
		t.Fatalf("FindHome = %v; want ErrHoming", err)
	}
	if sl, _ := m.Slewing(); sl {
		t.Error("Slewing latched on a :Ch# the firmware discarded")
	}
}

// A failed :AH# read must not refuse a halt. Blocking a stop because a status read failed is
// worse than the collision the guard exists to prevent.
func TestHaltProceedsWhenTheBusyFlagCannotBeRead(t *testing.T) {
	m, f := newMount(map[string]string{":AH#": "garbage#"})
	if err := m.Halt(); err != nil {
		t.Fatalf("Halt: %v", err)
	}
	var sawQ bool
	for _, w := range f.Writes() {
		if w == ":Q#" {
			sawQ = true
		}
	}
	if !sawQ {
		t.Errorf("Halt wrote %q; want :Q# despite the unreadable busy flag", f.Writes())
	}
}
