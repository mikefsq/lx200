package rst

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mikefsq/lx200"
)

func TestSlewLimitSettersCoverEveryRegister(t *testing.T) {
	for i, c := range []byte{'a', 'b', 'c', 'd', 'e', 'f'} {
		m, f := newMount(nil)
		if err := m.SetSlewLimit(i, 12.5); err != nil {
			t.Fatalf("SetSlewLimit(%d): %v", i, err)
		}
		want := fmt.Sprintf(":C%c12.500#", c)
		if got := f.LastWrite(); got != want {
			t.Errorf("SetSlewLimit(%d) wrote %q, want %q", i, got, want)
		}
	}
}

func TestSlewLimitsReadsAllSix(t *testing.T) {
	m, _ := newMount(map[string]string{
		":CA#": ":CA085#", ":CB#": ":CB020#", ":CC#": ":CC-022.399#",
		":CD#": ":CD150#", ":CE#": ":CE020#", ":CF#": ":CF-006.900#",
	})
	got, err := m.SlewLimits()
	if err != nil {
		t.Fatalf("SlewLimits: %v", err)
	}
	for i, w := range [6]float64{85, 20, -22.399, 150, 20, -6.9} {
		if !near(got[i], w) {
			t.Errorf("SlewLimits[%d] = %v, want %v", i, got[i], w)
		}
	}
}

// :F, external SPI memory: every accepted second character, and rejection of the rest.
func TestSPIRawCoversTheFamily(t *testing.T) {
	for _, c := range []byte("BbCcEeFgrt") {
		m, f := newMount(nil)
		if err := m.Unsafe().SPIRaw(c, "42"); err != nil {
			t.Fatalf("SPIRaw(%c): %v", c, err)
		}
		want := fmt.Sprintf(":F%c42#", c)
		if got := f.LastWrite(); got != want {
			t.Errorf("SPIRaw(%c) wrote %q, want %q", c, got, want)
		}
	}
	for _, c := range []byte("AZxq") {
		m, f := newMount(nil)
		if err := m.Unsafe().SPIRaw(c, ""); err == nil {
			t.Errorf("SPIRaw(%c) = nil; want an error", c)
		}
		if f.LastWrite() != "" {
			t.Errorf("SPIRaw(%c) wrote %q; nothing should reach the mount", c, f.LastWrite())
		}
	}
}

// :X, factory diagnostics. These reach NVM, PEC and the encoder, so the guard matters as much
// as the format.
func TestDiagnosticCoversTheFamily(t *testing.T) {
	for _, c := range []byte("ABCDEeFGHPR") {
		m, f := newMount(nil)
		if err := m.Unsafe().Diagnostic(c, ""); err != nil {
			t.Fatalf("Diagnostic(%c): %v", c, err)
		}
		want := fmt.Sprintf(":X%c#", c)
		if got := f.LastWrite(); got != want {
			t.Errorf("Diagnostic(%c) wrote %q, want %q", c, got, want)
		}
	}
	for _, c := range []byte("ZYq1") {
		m, f := newMount(nil)
		if err := m.Unsafe().Diagnostic(c, ""); err == nil {
			t.Errorf("Diagnostic(%c) = nil; want an error", c)
		}
		if f.LastWrite() != "" {
			t.Errorf("Diagnostic(%c) wrote %q; nothing should reach the mount", c, f.LastWrite())
		}
	}
}

// :P, PEC. Absent from the 135E, so this is the only coverage available.
func TestPECCoversTheFamily(t *testing.T) {
	for _, c := range []byte("AaDFPpUu") {
		m, f := newMount(nil)
		if err := m.Unsafe().PEC(c); err != nil {
			t.Fatalf("PEC(%c): %v", c, err)
		}
		want := fmt.Sprintf(":P%c#", c)
		if got := f.LastWrite(); got != want {
			t.Errorf("PEC(%c) wrote %q, want %q", c, got, want)
		}
	}
	for _, c := range []byte("BCZ") {
		m, _ := newMount(nil)
		if err := m.Unsafe().PEC(c); err == nil {
			t.Errorf("PEC(%c) = nil; want an error", c)
		}
	}
}

// :V, the satellite flags that take no argument.
func TestSatelliteFlagsCoverTheFamily(t *testing.T) {
	for _, c := range []byte("TtUuV") {
		m, f := newMount(nil)
		if err := m.SatelliteFlag(c); err != nil {
			t.Fatalf("SatelliteFlag(%c): %v", c, err)
		}
		want := fmt.Sprintf(":V%c#", c)
		if got := f.LastWrite(); got != want {
			t.Errorf("SatelliteFlag(%c) wrote %q, want %q", c, got, want)
		}
	}
}

// A full satellite upload writes every field in the order the vendor tool uses. The family is
// write-only, so this sequence is the only description of it that exists.
func TestUploadSatelliteWritesEveryField(t *testing.T) {
	m, f := newMount(nil)
	err := m.UploadSatellite(3, Satellite{
		Name: "ISS", EpochYear: "26", EpochDay: "23750000",
		FirstDerivative: "00001234", Inclination: "051.6400",
		RightAscension: "247.4627", Eccentricity: "0.0006703",
		ArgumentOfPerigee: "130.5360", MeanAnomaly: "325.0288",
		MeanMotion: "15.72125391", Magnitude: "-1.8",
	})
	if err != nil {
		t.Fatalf("UploadSatellite: %v", err)
	}
	want := []string{
		":Vn03#", ":VE2623750000#", ":VP23750000#", ":Vd00001234#",
		":Vi051.6400#", ":Vo247.4627#", ":Ve0006703#", ":VO130.5360#",
		":VM325.0288#", ":VR15.72125391#", ":VNISS#", ":Vm-1.8#", ":VA03#",
	}
	got := f.Writes()
	if len(got) != len(want) {
		t.Fatalf("UploadSatellite wrote %d frames, want %d:\n got %q\nwant %q", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("frame %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// Eccentricity is sent without its "0." prefix. Either form must produce the same frame, since
// the mount cannot complain.
func TestSatelliteEccentricityNormalises(t *testing.T) {
	for _, in := range []string{"0.0006703", "0006703"} {
		m, f := newMount(nil)
		if err := m.UploadSatellite(0, Satellite{Eccentricity: in}); err != nil {
			t.Fatal(err)
		}
		var seen string
		for _, w := range f.Writes() {
			if len(w) > 3 && w[:3] == ":Ve" {
				seen = w
			}
		}
		if seen != ":Ve0006703#" {
			t.Errorf("Eccentricity %q produced %q, want :Ve0006703#", in, seen)
		}
	}
}

// The direction and rate commands are promoted from the embedded lx200.Conn, and users reach
// them through Mount. The RST has no directional halt; Halt sends a bare :Q#.
func TestPromotedMotionCommands(t *testing.T) {
	for _, d := range []lx200.Direction{lx200.North, lx200.South, lx200.East, lx200.West} {
		m, f := newMount(nil)
		if err := m.Move(d); err != nil {
			t.Fatalf("Move(%c): %v", d, err)
		}
		if want := fmt.Sprintf(":M%c#", d); f.LastWrite() != want {
			t.Errorf("Move(%c) wrote %q, want %q", d, f.LastWrite(), want)
		}

		m, f = newMount(nil)
		if err := m.HaltMove(d); err != nil {
			t.Fatalf("HaltMove(%c): %v", d, err)
		}
		if want := fmt.Sprintf(":Q%c#", d); f.LastWrite() != want {
			t.Errorf("HaltMove(%c) wrote %q, want %q", d, f.LastWrite(), want)
		}
	}
	for _, r := range []lx200.Rate{lx200.RateGuide, lx200.RateCenter, lx200.RateFind, lx200.RateMax} {
		m, f := newMount(nil)
		if err := m.SetRate(r); err != nil {
			t.Fatalf("SetRate(%c): %v", r, err)
		}
		if want := fmt.Sprintf(":R%c#", r); f.LastWrite() != want {
			t.Errorf("SetRate(%c) wrote %q, want %q", r, f.LastWrite(), want)
		}
	}
}

// The site-value pairs differ only in case.
func TestSiteValuePairs(t *testing.T) {
	cases := []struct {
		call func(m *Mount) error
		want string
	}{
		{func(m *Mount) error { return m.SetSiteValueL(1, true) }, ":WL0001#"},
		{func(m *Mount) error { return m.SetSiteValueL(1, false) }, ":Wl0001#"},
		{func(m *Mount) error { return m.SetSiteValueM(1, true) }, ":WM0001#"},
		{func(m *Mount) error { return m.SetSiteValueM(1, false) }, ":Wm0001#"},
	}
	for _, c := range cases {
		m, f := newMount(nil)
		if err := c.call(m); err != nil {
			t.Fatal(err)
		}
		if f.LastWrite() != c.want {
			t.Errorf("wrote %q, want %q", f.LastWrite(), c.want)
		}
	}
}

// Encoder-rate commands exist only on the 135E.
func TestEncoderRatePair(t *testing.T) {
	for _, c := range []byte("bc") {
		m, f := newMount(nil)
		if err := m.SetEncoderRate(c, "1 1"); err != nil {
			t.Fatalf("SetEncoderRate(%c): %v", c, err)
		}
		if want := fmt.Sprintf(":M%c1 1#", c); f.LastWrite() != want {
			t.Errorf("SetEncoderRate(%c) wrote %q, want %q", c, f.LastWrite(), want)
		}
	}
}

// The remaining commands, each with a real reply where one exists.

// FindHome checks the busy flag, then stops tracking, then seeks. Tracking has to go first
// because with it on, :Ch# aborts and pushes a :CH< fail token.
func TestFindHomeStopsTrackingFirst(t *testing.T) {
	m, f := newMount(map[string]string{":CtL#": ":CTL#"})
	if err := m.FindHome(); err != nil {
		t.Fatalf("FindHome: %v", err)
	}
	w := f.Writes()
	if len(w) < 3 || w[0] != ":AH#" || w[1] != ":CtL#" || w[2] != ":Ch#" {
		t.Errorf("FindHome wrote %q; want :AH# then :CtL# then :Ch#", w)
	}
	if slewing, _ := m.Slewing(); !slewing {
		t.Error("FindHome should latch slewing until the :CHO# token arrives")
	}
}

// :GH# reads O once the mount has homed since power-on. Open seeds HomeFound from it, so a
// reconnect to an already-homed mount does not refuse every slew.
func TestHomeLatchSeedsHomeFound(t *testing.T) {
	for rep, want := range map[string]bool{":GHO#": true, ":GH0#": false} {
		m, _ := newMount(map[string]string{":GH#": rep})
		got, err := m.homeLatched()
		if err != nil || got != want {
			t.Errorf("homeLatched(%s) = %v, %v; want %v", rep, got, err, want)
		}
	}
}

// SlewToAltAz sets azimuth, then altitude, then fires :MA#. Both setters are blind, so nothing
// on the wire would object to a wrong format.
func TestSlewToAltAzSequence(t *testing.T) {
	m, f := newMount(nil) // the fixture starts homed; :Sz/:Sa/:MA are all blind
	if err := m.SlewToAltAz(90, 0); err != nil {
		t.Fatalf("SlewToAltAz: %v", err)
	}
	w := f.Writes()
	want := []string{":Sz090*00'00.0#", ":Sa+00*00'00.0#", ":MA#"}
	if len(w) != len(want) {
		t.Fatalf("SlewToAltAz wrote %q, want %q", w, want)
	}
	for i := range want {
		if w[i] != want[i] {
			t.Errorf("frame %d = %q, want %q", i, w[i], want[i])
		}
	}
}

// :SC# is the only setter here with a compound reply: an ack byte, then two more frames. The
// ack alone proves nothing, so the date is confirmed by reading :GC# back.
func TestSetDate(t *testing.T) {
	m, f := newMount(map[string]string{
		":SC08/25/26#": "1",
		":GC#":         ":GC08/25/26#",
	})
	if err := m.SetDate(8, 25, 26); err != nil {
		t.Errorf("SetDate: %v", err)
	}
	var sawWrite bool
	for _, w := range f.Writes() {
		if w == ":SC08/25/26#" {
			sawWrite = true
		}
	}
	if !sawWrite {
		t.Errorf("SetDate wrote %q, want :SC08/25/26#", f.Writes())
	}
}

// A mount that acks and keeps its old date must surface as an error, not as success.
func TestSetDateFailsWhenTheReadbackDisagrees(t *testing.T) {
	m, _ := newMount(map[string]string{
		":SC08/25/26#": "1",
		":GC#":         ":GC01/01/20#", // unchanged
	})
	err := m.SetDate(8, 25, 26)
	if err == nil {
		t.Fatal("SetDate returned nil when the mount kept its old date")
	}
	if !strings.Contains(err.Error(), "01/01/20") {
		t.Errorf("SetDate error %q should name what the mount actually reports", err)
	}
}

// :CZ# answers an empty framed reply and then unframed text that borrows the next reply's '#'.
// DebugDumpZ spends a throwaway :GR# to supply the terminator and splits on the echoed prefix.
// The fixture reproduces the run-together frame seen on hardware.
func TestDebugDumpZSplitsOnTheEchoedPrefix(t *testing.T) {
	m, _ := newMount(map[string]string{
		":CZ#": ":CZ#",
		":GR#": " 127 127 127 127 0 0 0 0 0 -122:GR01:02:03.4#",
	})
	got, err := m.DebugDumpZ()
	if err != nil || got != "127 127 127 127 0 0 0 0 0 -122" {
		t.Errorf("DebugDumpZ = %q, %v; want the debug line with the :GR reply stripped", got, err)
	}
}

// With no debug text in front of it, the result must be empty rather than the borrowed reply.
func TestDebugDumpZEmptyWhenNothingPrecedes(t *testing.T) {
	m, _ := newMount(map[string]string{":CZ#": ":CZ#", ":GR#": ":GR01:02:03.4#"})
	got, err := m.DebugDumpZ()
	if err != nil || got != "" {
		t.Errorf("DebugDumpZ = %q, %v; want empty", got, err)
	}
}

// :CW# does not answer when sent bare, so the argument is part of the command.
func TestStatusWTakesAnArgument(t *testing.T) {
	m, f := newMount(map[string]string{":CW1#": ":CWWA00=00h:0m:0/++0::0::0#"})
	if _, err := m.StatusW("1"); err != nil {
		t.Fatalf("StatusW: %v", err)
	}
	if f.LastWrite() != ":CW1#" {
		t.Errorf("StatusW wrote %q, want :CW1#", f.LastWrite())
	}
}

// :WI# and :WJ# answer with an empty payload on the development mount, which must not error.
func TestSiteInfoEmptyPayload(t *testing.T) {
	m, _ := newMount(map[string]string{":WI#": ":WI#", ":WJ#": ":WJ#"})
	if got, err := m.SiteInfoI(); err != nil || got != ":WI" {
		t.Errorf("SiteInfoI = %q, %v", got, err)
	}
	if got, err := m.SiteInfoJ(); err != nil || got != ":WJ" {
		t.Errorf("SiteInfoJ = %q, %v", got, err)
	}
}

// :CM# is the plain Meade sync, to wherever the mount currently is, rather than the
// coordinate-carrying :Ck# SyncToTarget uses. It answers the same CM verdict.
func TestSyncCurrent(t *testing.T) {
	m, f := newMount(map[string]string{":CM#": ":CMSynced#"})
	got, err := m.SyncCurrent()
	if err != nil || got != "Synced" {
		t.Errorf("SyncCurrent = %q, %v; want \"Synced\"", got, err)
	}
	if f.LastWrite() != ":CM#" {
		t.Errorf("SyncCurrent wrote %q, want :CM#", f.LastWrite())
	}

	m, _ = newMount(map[string]string{":CM#": ":CMF#"})
	if _, err := m.SyncCurrent(); err == nil {
		t.Error("SyncCurrent = nil error on a CMF refusal; want an error")
	}
}

// The protocol has no site-elevation command, since elevation does not affect pointing,
// so SetSiteElevation exists only to satisfy lx200.SiteSetter. It must send nothing at all
// rather than invent a frame.
func TestSetSiteElevationSendsNothing(t *testing.T) {
	m, f := newMount(nil)
	if err := m.SetSiteElevation(123); err != nil {
		t.Errorf("SetSiteElevation = %v; want nil", err)
	}
	if got := f.LastWrite(); got != "" {
		t.Errorf("SetSiteElevation wrote %q; the RST has no such command", got)
	}
}

// SetAxisRate must write the speed slot BEFORE selecting the preset that reads it. Selecting
// alone picks up whatever the slot happens to hold, 1500x on the development mount against the
// 2000x the vendor uses, so the frame order is the behaviour under test, not
// just the frames.
func TestSetAxisRateWritesTheSlotThenSelectsIt(t *testing.T) {
	cases := []struct {
		rate float64
		want []string
	}{
		{AxisRateMax, []string{":Cu3=2000#", ":RS#"}},
		{9.0, []string{":Cu3=2000#", ":RS#"}}, // clamped to the vendor's literal
		{AxisRateFast, []string{":Cu3=0600#", ":RS#"}},
		{AxisRateMedium, []string{":Cu2=0200#", ":RM#"}},
		{AxisRateSlow, []string{":Cu1=0001#", ":RC#"}},
	}
	for _, c := range cases {
		m, f := newMount(nil)
		if err := m.SetAxisRate(c.rate); err != nil {
			t.Fatalf("SetAxisRate(%g): %v", c.rate, err)
		}
		got := f.Writes()
		if len(got) != len(c.want) {
			t.Fatalf("SetAxisRate(%g) wrote %q, want %q", c.rate, got, c.want)
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("SetAxisRate(%g) frame %d = %q, want %q", c.rate, i, got[i], c.want[i])
			}
		}
	}
}

// A rate below the slowest preset falls through to the guide slot, and zero stops.
func TestSetAxisRateEdges(t *testing.T) {
	m, f := newMount(nil)
	if err := m.SetAxisRate(0.001); err != nil {
		t.Fatalf("SetAxisRate(0.001): %v", err)
	}
	got := f.Writes()
	if len(got) != 2 || got[1] != ":RG#" {
		t.Errorf("SetAxisRate(0.001) wrote %q; want a :Cu0= write then :RG#", got)
	}

	m, f = newMount(nil)
	if err := m.SetAxisRate(0); err != nil {
		t.Fatalf("SetAxisRate(0): %v", err)
	}
	if got := f.LastWrite(); got != ":Q#" {
		t.Errorf("SetAxisRate(0) wrote %q, want :Q#", got)
	}
}

// The slot setter bounds its arguments: the wire field is four digits and there are three
// slots. A malformed frame here would be silent.
func TestSetSlewSpeedRejectsOutOfRange(t *testing.T) {
	m, f := newMount(nil)
	for _, c := range []struct {
		n, v int
	}{{0, 100}, {4, 100}, {1, -1}, {1, 10000}} {
		if err := m.SetSlewSpeed(c.n, c.v); err == nil {
			t.Errorf("SetSlewSpeed(%d, %d) = nil; want an error", c.n, c.v)
		}
	}
	if f.LastWrite() != "" {
		t.Errorf("wrote %q; nothing should reach the mount", f.LastWrite())
	}
}

// The four advertised rates convert to the multiples the vendor sends.
func TestAxisRatesConvertToVendorMultiples(t *testing.T) {
	want := map[float64]int{AxisRateMax: 2000, AxisRateFast: 600, AxisRateMedium: 200, AxisRateSlow: 1}
	for _, r := range AxisRates() {
		if got := int(r*siderealPerDegSec + 0.5); got != want[r] {
			t.Errorf("%g deg/s = %dx sidereal, want %dx", r, got, want[r])
		}
	}
}
