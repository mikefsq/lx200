package am5

import (
	"math"
	"testing"
	"time"
)

// Every reply below is one a real AM5 gave, taken from the captures in docs/am5/ — so these pin
// the parse against the mount rather than against an assumption about it.

func TestMAC(t *testing.T) {
	m, _ := newMount(map[string]string{":GMA#": "f412fa60f72d#"})
	got, err := m.MAC()
	if err != nil || got != "f412fa60f72d" {
		t.Errorf("MAC = %q, %v; want f412fa60f72d", got, err)
	}
}

// Longitude is Meade-reversed in both directions: the mount answers west-positive, we speak
// east-positive, and a site that reads back must match what was set.
func TestSiteReadbackReversesLongitude(t *testing.T) {
	m, _ := newMount(map[string]string{":Gt#": "+37*44:49#", ":Gg#": "+122*24:58#"})
	lat, err := m.SiteLatitude()
	if err != nil || math.Abs(lat-37.7469) > 1e-3 {
		t.Errorf("SiteLatitude = %v, %v; want ≈ +37.747", lat, err)
	}
	lon, err := m.SiteLongitude()
	if err != nil || math.Abs(lon-(-122.4161)) > 1e-3 {
		t.Errorf("SiteLongitude = %v, %v; want ≈ −122.416 (east-positive)", lon, err)
	}
}

// SetSite is the vendor's single command, and it reverses longitude the same way the separate
// setters do.
func TestSetSiteCombined(t *testing.T) {
	want := ":SMGE+37*44:49&+122*24:58#"
	m, f := newMount(map[string]string{want: "1"})
	if err := m.SetSite(37.7469, -122.4161); err != nil {
		t.Fatalf("SetSite: %v", err)
	}
	if w := f.Writes(); len(w) != 1 || w[0] != want {
		t.Errorf("SetSite wrote %v, want [%s]", w, want)
	}
}

// The mount's offset convention is the reverse of a Go zone offset: :GG# answers the hours added
// to local to yield UTC, so +08:00 is UTC−8.
func TestUTCOffsetIsNegated(t *testing.T) {
	m, _ := newMount(map[string]string{":GG#": "+08:00#"})
	off, err := m.UTCOffset()
	if err != nil || off != -8*time.Hour {
		t.Errorf("UTCOffset = %v, %v; want -8h", off, err)
	}
}

// Clock is the read half of SetUTC: date, time of day and offset as one instant, in the mount's
// own zone.
func TestClock(t *testing.T) {
	m, _ := newMount(map[string]string{
		":GC#": "09/10/26#", ":GL#": "13:08:10#", ":GG#": "+08:00#",
	})
	got, err := m.Clock()
	if err != nil {
		t.Fatalf("Clock: %v", err)
	}
	if y, mo, d := got.Date(); y != 2026 || mo != time.September || d != 10 {
		t.Errorf("date = %d-%02d-%02d, want 2026-09-10", y, mo, d)
	}
	if h, mi, s := got.Clock(); h != 13 || mi != 8 || s != 10 {
		t.Errorf("time = %02d:%02d:%02d, want 13:08:10", h, mi, s)
	}
	if _, off := got.Zone(); off != -8*3600 {
		t.Errorf("zone offset = %ds, want -28800", off)
	}
	// The absolute instant is what a caller comparing against time.Now() needs.
	if want := time.Date(2026, 9, 10, 21, 8, 10, 0, time.UTC); !got.UTC().Equal(want) {
		t.Errorf("UTC = %v, want %v", got.UTC(), want)
	}
}

func TestSetLocalTime(t *testing.T) {
	want := ":SL13:00:26#"
	m, f := newMount(map[string]string{want: "1"})
	if err := m.SetLocalTime(time.Date(2026, 9, 10, 13, 0, 26, 0, time.UTC)); err != nil {
		t.Fatalf("SetLocalTime: %v", err)
	}
	if w := f.Writes(); len(w) != 1 || w[0] != want {
		t.Errorf("SetLocalTime wrote %v, want [%s]", w, want)
	}
}

// The index is returned raw. What its values mean is not established — every capture answers 0 —
// and a guessed mapping onto DriveRate would be worse than none.
func TestTrackingRateIndex(t *testing.T) {
	m, _ := newMount(map[string]string{":GT#": "0#"})
	if i, err := m.TrackingRateIndex(); err != nil || i != 0 {
		t.Errorf("TrackingRateIndex = %d, %v; want 0", i, err)
	}
}

// Heavy-duty mode reports itself as the maximum rate, not as a flag.
func TestHeavyDuty(t *testing.T) {
	m, _ := newMount(map[string]string{":GRl#": "1440#"})
	if on, err := m.HeavyDuty(); err != nil || on {
		t.Errorf("HeavyDuty = %v, %v; want false (1440)", on, err)
	}
	m, _ = newMount(map[string]string{":GRl#": "720#"})
	if on, err := m.HeavyDuty(); err != nil || !on {
		t.Errorf("HeavyDuty = %v, %v; want true (720)", on, err)
	}
	// An unexpected value is an error rather than a silent false: the reply is a rate, and a rate
	// we do not recognise means the assumption behind this pair is wrong.
	m, _ = newMount(map[string]string{":GRl#": "900#"})
	if _, err := m.HeavyDuty(); err == nil {
		t.Error("HeavyDuty accepted an unrecognised rate")
	}
}

func TestSetHeavyDuty(t *testing.T) {
	m, f := newMount(nil)
	if err := m.SetHeavyDuty(true); err != nil {
		t.Fatal(err)
	}
	if err := m.SetHeavyDuty(false); err != nil {
		t.Fatal(err)
	}
	w := f.Writes()
	if len(w) != 2 || w[0] != ":SRl720#" || w[1] != ":SRl1440#" {
		t.Errorf("wrote %v, want [:SRl720# :SRl1440#]", w)
	}
}

// A date with no century is read as 20YY, which is what the mount does with :SC#.
func TestLocalDateCentury(t *testing.T) {
	m, _ := newMount(map[string]string{":GC#": "09/10/26#"})
	y, mo, d, err := m.LocalDate()
	if err != nil || y != 2026 || mo != time.September || d != 10 {
		t.Errorf("LocalDate = %d-%v-%d, %v; want 2026-September-10", y, mo, d, err)
	}
	m, _ = newMount(map[string]string{":GC#": "garbage#"})
	if _, _, _, err := m.LocalDate(); err == nil {
		t.Error("LocalDate accepted a malformed reply")
	}
}
