package rst

import (
	"math"
	"strings"
	"testing"
	"time"
)

// Reply-parsing coverage for every read command.
//

func near(a, b float64) bool { return math.Abs(a-b) < 1e-3 }

// Coordinates and time, all of which echo the command prefix.
func TestParseCoordinateReplies(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		rep  string
		call func(m *Mount) (float64, error)
		want float64
	}{
		{"RA", ":GR#", ":GR01:01:47.7#", (*Mount).RA, 1.029917},
		{"Dec", ":GD#", ":GD-16*03'40.1#", (*Mount).Dec, -16.061139},
		{"Altitude", ":GA#", ":GA-00*52'29.5#", (*Mount).Altitude, -0.874861},
		{"Azimuth", ":GZ#", ":GZ270*40'42.3#", (*Mount).Azimuth, 270.678417},
		{"SiderealTime", ":GS#", ":GS08:40:22#", (*Mount).SiderealTime, 8.672778},
		{"LocalTime", ":GL#", ":GL11:43:55#", (*Mount).LocalTime, 11.731944},
		{"SiteLatitude", ":Gt#", ":Gt+37*46'37.9#", (*Mount).SiteLatitude, 37.777194},
		{"TargetRA", ":Gr#", ":Gr05:34:30.0#", (*Mount).TargetRA, 5.575},
		{"TargetDec", ":Gd#", ":Gd+22*00'59.9#", (*Mount).TargetDec, 22.016639},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _ := newMount(map[string]string{c.cmd: c.rep})
			got, err := c.call(m)
			if err != nil || !near(got, c.want) {
				t.Errorf("%s(%s) = %v, %v; want %v", c.name, c.rep, got, err, c.want)
			}
		})
	}
}

// Longitude is east-negative on the wire and east-positive in the API, so the sign flips. A
// mistake here puts the site on the other side of the planet and every reply still parses.
func TestSiteLongitudeFlipsSign(t *testing.T) {
	m, _ := newMount(map[string]string{":Gg#": ":Gg+122*24'40.0#"})
	got, err := m.SiteLongitude()
	if err != nil || !near(got, -122.411111) {
		t.Errorf("SiteLongitude = %v, %v; want -122.411 (wire +122 means 122 WEST)", got, err)
	}
}

// The UTC offset is hours to add to local time to reach UTC, so a Pacific mount reports +7.
func TestUTCOffset(t *testing.T) {
	m, _ := newMount(map[string]string{":GG#": ":GG+07#"})
	got, err := m.UTCOffset()
	if err != nil || !near(got, 7) {
		t.Errorf("UTCOffset = %v, %v; want +7", got, err)
	}
}

func TestDate(t *testing.T) {
	m, _ := newMount(map[string]string{":GC#": ":GC08/25/26#"})
	got, err := m.Date()
	if err != nil || got != "08/25/26" {
		t.Errorf("Date = %q, %v; want \"08/25/26\"", got, err)
	}
}

// Telemetry with multi-field replies, the parsers most likely to go quietly wrong.
func TestTelemetryReplies(t *testing.T) {
	m, _ := newMount(map[string]string{":Cv#": ":Cv11.9#"})
	if v, err := m.Voltage(); err != nil || !near(v, 11.9) {
		t.Errorf("Voltage = %v, %v; want 11.9", v, err)
	}

	m, _ = newMount(map[string]string{":CP#": ":CP+002.5|+006.0#"})
	dec, ra, err := m.MotorLoad()
	if err != nil || !near(dec, 2.5) || !near(ra, 6.0) {
		t.Errorf("MotorLoad = %v, %v, %v; want 2.5, 6.0", dec, ra, err)
	}

	// The reply seen on hardware: TCS ok, GPS with no fix, both motors ok.
	m, _ = newMount(map[string]string{":GY#": ":GYOXOO#"})
	st, err := m.SystemStatus()
	if err != nil || !st.TCS || !st.DecMotor || !st.RAMotor || st.Raw != "OXOO" {
		t.Errorf("SystemStatus(:GYOXOO#) = %+v, %v; want TCS and both motors ok", st, err)
	}
	if st.GPS != GPSNone {
		t.Errorf("GPS = %v; want none — flag 1 is 'X' in this reply", st.GPS)
	}

	// The GPS flag is tri-state, which is why it is not a bool: T means time but no fix.
	for rep, want := range map[string]GPSState{
		":GYOXOO#": GPSNone, ":GYOTOO#": GPSTimeOnly, ":GYOOOO#": GPSFix,
	} {
		m, _ := newMount(map[string]string{":GY#": rep})
		got, err := m.SystemStatus()
		if err != nil || got.GPS != want {
			t.Errorf("SystemStatus(%s).GPS = %v, %v; want %v", rep, got.GPS, err, want)
		}
	}

	// A fault must clear exactly one motor flag. The Dec/RA mapping of indices
	// 2 and 3 has not been verified on a faulted mount.
	m, _ = newMount(map[string]string{":GY#": ":GYOXXO#"})
	if st, err = m.SystemStatus(); err != nil || st.DecMotor == st.RAMotor {
		t.Errorf("SystemStatus(:GYOXXO#) = %+v, %v; want exactly one motor flag cleared", st, err)
	}
}

// Auto-resume answers CRR when on and CRX when off.
func TestAutoResume(t *testing.T) {
	for rep, want := range map[string]bool{":CRR#": true, ":CRX#": false} {
		m, _ := newMount(map[string]string{":CR#": rep})
		if got, err := m.AutoResume(); err != nil || got != want {
			t.Errorf("AutoResume(%s) = %v, %v; want %v", rep, got, err, want)
		}
	}
}

// Guide and slew speeds share the ":CU<n>=" reply shape.
func TestSpeeds(t *testing.T) {
	m, _ := newMount(map[string]string{":CU0#": ":CU0=0.5#"})
	if v, err := m.GuideRate(); err != nil || !near(v, 0.5) {
		t.Errorf("GuideRate = %v, %v; want 0.5", v, err)
	}
	m, _ = newMount(map[string]string{":CU1#": ":CU1=0100#"})
	if v, err := m.SlewSpeed(1); err != nil || v != 100 {
		t.Errorf("SlewSpeed(1) = %v, %v; want 100", v, err)
	}
}

// Tracking state and rate.
func TestTrackingReplies(t *testing.T) {
	for rep, want := range map[string]bool{":AT1#": true, ":AT0#": false} {
		m, _ := newMount(map[string]string{":AT#": rep})
		if got, err := m.Tracking(); err != nil || got != want {
			t.Errorf("Tracking(%s) = %v, %v; want %v", rep, got, err, want)
		}
	}
	m, _ := newMount(map[string]string{":Ct?#": ":CT0#"})
	if got, err := m.TrackMode(); err != nil || got != TrackModeSidereal {
		t.Errorf("TrackMode = %v, %v; want sidereal", got, err)
	}
}

// The bare-reply group: no echoed prefix, so there is nothing to match on and a misread would
// look like a valid value.
func TestBareReplies(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		rep  string
		call func(m *Mount) (float64, error)
		want float64
	}{
		{"FieldDiameter", ":GF#", "600'#", (*Mount).FieldDiameter, 600},
		{"BrightMagnitudeLimit", ":Gb#", "+05.0#", (*Mount).BrightMagnitudeLimit, 5},
		{"FaintMagnitudeLimit", ":Gf#", "-09.9#", (*Mount).FaintMagnitudeLimit, -9.9},
		{"HigherAltitudeLimit", ":Gh#", "00*#", (*Mount).HigherAltitudeLimit, 0},
		{"LargerSizeLimit", ":Gl#", "000'#", (*Mount).LargerSizeLimit, 0},
		{"SmallerSizeLimit", ":Gs#", "999'#", (*Mount).SmallerSizeLimit, 999},
		{"TrackingRateHz", ":GT#", "60.0#", (*Mount).TrackingRateHz, 60},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _ := newMount(map[string]string{c.cmd: c.rep})
			got, err := c.call(m)
			if err != nil || !near(got, c.want) {
				t.Errorf("%s(%s) = %v, %v; want %v", c.name, c.rep, got, err, c.want)
			}
		})
	}

	m, _ := newMount(map[string]string{":Gq#": "SU#"})
	if q, err := m.MinimumQuality(); err != nil || q != QualitySuperb {
		t.Errorf("MinimumQuality = %q, %v; want SU", q, err)
	}
	m, _ = newMount(map[string]string{":Gy#": "G#"})
	if s, err := m.SearchString(); err != nil || s != "G" {
		t.Errorf("SearchString = %q, %v; want G", s, err)
	}
	m, _ = newMount(map[string]string{":Gc#": "12#"})
	if v, err := m.ClockFormat(); err != nil || v != 12 {
		t.Errorf("ClockFormat = %v, %v; want 12", v, err)
	}
}

// Identity and configuration.
func TestIdentityReplies(t *testing.T) {
	m, _ := newMount(map[string]string{":AV#": ":AV260319#"})
	if v, err := m.Version(); err != nil || v != "260319" {
		t.Errorf("Version = %q, %v; want 260319", v, err)
	}
	m, _ = newMount(map[string]string{":AM#": ":AM#"})
	if v, err := m.ModelName(); err != nil || v != "" {
		t.Errorf("ModelName = %q, %v; want empty", v, err)
	}
	m, _ = newMount(map[string]string{":AF#": ":AF0#"})
	if v, err := m.ForcePierFlip(); err != nil || v {
		t.Errorf("ForcePierFlip = %v, %v; want false", v, err)
	}
	m, _ = newMount(map[string]string{":GP#": "H#"})
	if v, err := m.Precision(); err != nil || v != "H" {
		t.Errorf("Precision = %q, %v; want H", v, err)
	}
	m, _ = newMount(map[string]string{":CG3#": ":CG3+000.0000#"})
	if v, err := m.PierSideOffset(); err != nil || !near(v, 0) {
		t.Errorf("PierSideOffset = %v, %v; want 0", v, err)
	}
}

// The unresolved commands still get their reply shapes pinned, so a parsing change is caught
// even though the meaning is unknown.
func TestUnresolvedReplyShapes(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		rep  string
		call func(m *Mount) (string, error)
		want string
	}{
		{"CounterI", ":CI#", ":CI00000#", (*Mount).CounterI, "00000"},
		{"CounterJ", ":CJ#", ":CJ00000#", (*Mount).CounterJ, "00000"},
		{"GotoOffsetFlag", ":CL#", ":CL0#", (*Mount).GotoOffsetFlag, "0"},
		{"SiteSlot", ":WA#", ":WA9#", (*Mount).SiteSlot, ":WA9"},
		{"SiteStatusQ", ":WQ#", ":WQ9#", (*Mount).SiteStatusQ, ":WQ9"},
		{"MotionP", ":MP#", ":MP1#", (*Mount).MotionP, "1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _ := newMount(map[string]string{c.cmd: c.rep})
			got, err := c.call(m)
			if err != nil || got != c.want {
				t.Errorf("%s(%s) = %q, %v; want %q", c.name, c.rep, got, err, c.want)
			}
		})
	}
}

// Site names carry no echo prefix and arrive space-padded.
func TestSiteNames(t *testing.T) {
	m, _ := newMount(map[string]string{":GM#": "My Home   #", ":GN#": "Hubo Lab. #", ":GO#": "Site 1    #"})
	for i, want := range map[int]string{1: "My Home", 2: "Hubo Lab.", 3: "Site 1"} {
		if got, err := m.SiteName(i); err != nil || got != want {
			t.Errorf("SiteName(%d) = %q, %v; want %q", i, got, err, want)
		}
	}
}

// SetUTC converts through the mount's own offset rather than the caller's timezone, because
// the handset owns the site's civil time.
func TestSetUTCUsesTheMountsOffset(t *testing.T) {
	m, f := newMount(map[string]string{":GG#": ":GG+07#", ":GC#": ":GC08/25/26#"})
	utc := time.Date(2026, 8, 25, 14, 24, 45, 0, time.UTC)
	if err := m.SetUTC(utc); err != nil {
		t.Fatalf("SetUTC: %v", err)
	}
	var sawTime, sawDate bool
	for _, w := range f.Writes() {
		switch {
		case w == ":SL07:24:45#":
			sawTime = true
		case strings.HasPrefix(w, ":SC"):
			sawDate = true
		}
	}
	if !sawTime {
		t.Errorf("SetUTC wrote %q; want :SL07:24:45# (14:24:45 UTC minus the mount's +7)", f.Writes())
	}
	if sawDate {
		t.Error("SetUTC sent :SC# with the date already correct — that triggers a planetary-data recompute for nothing")
	}
}

// A wrong date is a silent pointing error, since the date feeds sidereal time. When it really
// differs, SetUTC must correct it, comparing against the local date the mount holds.
func TestSetUTCCorrectsTheDateWhenItDiffers(t *testing.T) {
	m, f := newMount(map[string]string{
		":GG#":         ":GG+07#",
		":GC#":         ":GC01/01/20#", // mount is years out
		":SC08/25/26#": "1",
	})
	utc := time.Date(2026, 8, 25, 14, 24, 45, 0, time.UTC)
	err := m.SetUTC(utc)
	// The readback is seeded stale on purpose. What matters is that the write was attempted.
	var sawDate bool
	for _, w := range f.Writes() {
		if w == ":SC08/25/26#" {
			sawDate = true
		}
	}
	if !sawDate {
		t.Errorf("SetUTC wrote %q; want :SC08/25/26# — the mount's date was 01/01/20", f.Writes())
	}
	if err == nil {
		t.Error("SetUTC returned nil although the date readback still disagreed")
	}
}

// 06:00 UTC at -7 is the previous day locally. The comparison has to use the local date, or
// SetUTC rewrites it, and triggers the
// recompute, once a day for no reason.
func TestSetUTCComparesTheLocalDateNotTheUTCDate(t *testing.T) {
	m, f := newMount(map[string]string{":GG#": ":GG+07#", ":GC#": ":GC08/24/26#"})
	utc := time.Date(2026, 8, 25, 6, 0, 0, 0, time.UTC) // 23:00 on the 24th, local
	if err := m.SetUTC(utc); err != nil {
		t.Fatalf("SetUTC: %v", err)
	}
	for _, w := range f.Writes() {
		if strings.HasPrefix(w, ":SC") {
			t.Errorf("SetUTC wrote %q; local date is still 08/24/26, so the mount is correct", w)
		}
	}
}

// The mount's UTC offset field is whole hours, so a fractional offset must be refused rather
// than truncated to the wrong zone.
func TestSetUTCOffsetRejectsFractionalHours(t *testing.T) {
	m, f := newMount(nil)
	if err := m.SetUTCOffset(5*time.Hour + 30*time.Minute); err == nil {
		t.Error("SetUTCOffset(5h30m) = nil; want an error, the wire field is whole hours")
	}
	if f.LastWrite() != "" {
		t.Errorf("wrote %q; nothing should reach the mount", f.LastWrite())
	}
}
