package tenmicron

import (
	"math"
	"testing"
	"time"
)

func TestSiteReadback(t *testing.T) {
	m, _ := newMount(map[string]string{
		":Gt#":  "+45:30:00.0#",
		":Gg#":  "-123:30:00.0#", // mount East-negative -> +123.5 East-positive
		":Gev#": "+0100.0#",
	})
	if v, err := m.SiteLatitude(); err != nil || math.Abs(v-45.5) > 1e-6 {
		t.Errorf("SiteLatitude = %v, %v; want 45.5", v, err)
	}
	if v, err := m.SiteLongitude(); err != nil || math.Abs(v-123.5) > 1e-6 {
		t.Errorf("SiteLongitude = %v, %v; want 123.5 (East-positive)", v, err)
	}
	if v, err := m.SiteElevation(); err != nil || math.Abs(v-100) > 1e-6 {
		t.Errorf("SiteElevation = %v, %v; want 100", v, err)
	}
}

func TestTimeReadback(t *testing.T) {
	m, _ := newMount(map[string]string{
		":GG#":   "-05:30:00.0#",
		":GDUT#": "0.1#",
		":GJD#":  "2459580.5#",
		":GUDT#": "06/02/26,15:04:05.0#",
	})
	if d, err := m.UTCOffset(); err != nil || d != -5*time.Hour-30*time.Minute {
		t.Errorf("UTCOffset = %v, %v; want -5h30m", d, err)
	}
	if v, err := m.UT1MinusUTC(); err != nil || math.Abs(v-0.1) > 1e-9 {
		t.Errorf("UT1MinusUTC = %v, %v; want 0.1", v, err)
	}
	if v, err := m.JulianDate(); err != nil || v != 2459580.5 {
		t.Errorf("JulianDate = %v, %v; want 2459580.5", v, err)
	}
	tm, err := m.UTCDateTime()
	if err != nil || !tm.Equal(time.Date(2026, 6, 2, 15, 4, 5, 0, time.UTC)) {
		t.Errorf("UTCDateTime = %v, %v; want 2026-06-02 15:04:05 UTC", tm, err)
	}
}

func TestLocalDateTimeGetters(t *testing.T) {
	m, _ := newMount(map[string]string{
		":GC#":   "06/02/26#",
		":GL#":   "15:04:05#",
		":GLDT#": "06/02/26,15:04:05.0#",
	})
	if d, err := m.LocalDate(); err != nil || !d.Equal(time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("LocalDate = %v, %v; want 2026-06-02", d, err)
	}
	if d, err := m.LocalTime(); err != nil || d != 15*time.Hour+4*time.Minute+5*time.Second {
		t.Errorf("LocalTime = %v, %v; want 15h04m05s", d, err)
	}
	if tm, err := m.LocalDateTime(); err != nil || !tm.Equal(time.Date(2026, 6, 2, 15, 4, 5, 0, time.UTC)) {
		t.Errorf("LocalDateTime = %v, %v; want 2026-06-02 15:04:05", tm, err)
	}
}

func TestGPS(t *testing.T) {
	m, _ := newMount(map[string]string{
		":gT#":  "1#",
		":gps#": "$GPGGA,123519,4807.038,N#",
		":gtg#": "1#",
	})
	if ok, err := m.UpdateFromGPS(); err != nil || !ok {
		t.Errorf("UpdateFromGPS = %v, %v; want true", ok, err)
	}
	if s, err := m.GPSNMEA(); err != nil || s != "$GPGGA,123519,4807.038,N" {
		t.Errorf("GPSNMEA = %q, %v", s, err)
	}
	if ok, err := m.GPSSynced(); err != nil || !ok {
		t.Errorf("GPSSynced = %v, %v; want true", ok, err)
	}
}
