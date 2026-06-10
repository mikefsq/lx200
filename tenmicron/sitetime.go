package tenmicron

import (
	"fmt"
	"strings"
	"time"

	"github.com/mikefsq/lx200"
)

// SetSiteLatitude / SetSiteLongitude / SetSiteElevation set the observing site
// (lx200.SiteSetter). 10Micron expresses East longitude as negative, so longitude
// is negated from the Alpaca East-positive convention.
func (m *Mount) SetSiteLatitude(deg float64) error {
	return must(m.Ack(":St" + dms(deg, 2) + "#"))
}

func (m *Mount) SetSiteLongitude(deg float64) error {
	return must(m.Ack(":Sg" + dms(-deg, 3) + "#")) // negate: 10Micron East = negative
}

func (m *Mount) SetSiteElevation(meters float64) error {
	return must(m.Ack(fmt.Sprintf(":Sev%+07.1f#", meters)))
}

// SiteLatitude reads the mount's configured site latitude in degrees (:Gt#).
func (m *Mount) SiteLatitude() (float64, error) { return m.getAngle(":Gt#") }

// SiteLongitude reads the site longitude in degrees, East-positive (:Gg#). The
// mount reports East as negative, so this negates it to the standard convention
// (matching SetSiteLongitude's input).
func (m *Mount) SiteLongitude() (float64, error) {
	v, err := m.getAngle(":Gg#")
	return -v, err
}

// SiteElevation reads the site elevation in metres (:Gev#).
func (m *Mount) SiteElevation() (float64, error) { return m.getFloat(":Gev#") }

// SetUTC sets the combined UTC date+time (:SUDT…#).
func (m *Mount) SetUTC(t time.Time) error {
	return must(m.Ack(t.UTC().Format(":SUDT2006-01-02,15:04:05#")))
}

// SetUTCOffset sets the offset added to local time to yield UTC (:SG). Use a zero
// offset — the mount's default working mode — so its local clock equals UTC and
// SetDate/SetTime below operate directly in UTC.
func (m *Mount) SetUTCOffset(offset time.Duration) error {
	sign := byte('+')
	if offset < 0 {
		sign, offset = '-', -offset
	}
	s := int(offset / time.Second)
	return must(m.Ack(fmt.Sprintf(":SG%c%02d:%02d:%02d#", sign, s/3600, (s/60)%60, s%60)))
}

// SetDate sets the mount's date (:SC), expressed in local time; with a zero UTC
// offset this is the UTC date. t is read in UTC. In the :U2# ultra-precision mode
// set at Connect, :SC replies a bare "1" (no "Updating Planetary Data" tail), so
// the 1-byte ack is safe.
func (m *Mount) SetDate(t time.Time) error {
	return must(m.Ack(t.UTC().Format(":SC2006-01-02#")))
}

// SetTime sets the mount's local time (:SL); with a zero UTC offset this is the
// UTC time. t is read in UTC.
func (m *Mount) SetTime(t time.Time) error {
	return must(m.Ack(t.UTC().Format(":SL15:04:05#")))
}

// UTCOffset reads the offset added to local time to obtain UTC (:GG#). Positive
// for west-of-Greenwich longitudes (the LX200 convention).
func (m *Mount) UTCOffset() (time.Duration, error) {
	s, err := m.Get(":GG#")
	if err != nil {
		return 0, err
	}
	h, err := lx200.ParseSexagesimal(strings.TrimSpace(s)) // sHH:MM:SS.S or sHH.H -> hours
	if err != nil {
		return 0, err
	}
	return time.Duration(h * float64(time.Hour)), nil
}

// UT1MinusUTC reads the current UT1−UTC difference in seconds (:GDUT#).
func (m *Mount) UT1MinusUTC() (float64, error) { return m.getFloat(":GDUT#") }

// JulianDate reads the mount's current Julian Date, computed from UTC (:GJD#).
func (m *Mount) JulianDate() (float64, error) { return m.getFloat(":GJD#") }

// UTCDateTime reads the mount's UTC date and time (:GUDT#).
func (m *Mount) UTCDateTime() (time.Time, error) { return m.dateTime(":GUDT#") }

// LocalDateTime reads the mount's local date and time (:GLDT#), returned as a
// time.Time in UTC location holding the local wall-clock value (the mount applies
// no zone).
func (m *Mount) LocalDateTime() (time.Time, error) { return m.dateTime(":GLDT#") }

// dateTime reads a "<date>,<time>#" reply (:GUDT#/:GLDT#). The format depends on
// the mount's emulation mode (date MM/DD/YY or MM:DD:YY, time HH:MM:SS[.S]), so
// several layouts are tried.
func (m *Mount) dateTime(cmd string) (time.Time, error) {
	s, err := m.Get(cmd)
	if err != nil {
		return time.Time{}, err
	}
	s = strings.TrimSpace(s)
	for _, layout := range []string{
		"01/02/06,15:04:05.0", "01/02/06,15:04:05",
		"01:02:06,15:04:05.0", "01:02:06,15:04:05",
	} {
		if t, e := time.Parse(layout, s); e == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("gotenmicron: unrecognized %s date/time %q", cmd, s)
}

// LocalDate reads the mount's current local date (:GC#). The reply format is
// emulation-dependent (MM/DD/YY or MM:DD:YY); both are parsed. The time fields are
// zero.
func (m *Mount) LocalDate() (time.Time, error) {
	s, err := m.Get(":GC#")
	if err != nil {
		return time.Time{}, err
	}
	s = strings.TrimSpace(s)
	for _, layout := range []string{"01/02/06", "01:02:06"} {
		if t, e := time.Parse(layout, s); e == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("gotenmicron: unrecognized :GC# date %q", s)
}

// LocalTime reads the mount's current local time as a duration since midnight
// (:GL#). The reply format is emulation/precision-dependent (HH:MM:SS,
// HH:MM:SS.S, or HH:MM.T); all parse via the sexagesimal reader.
func (m *Mount) LocalTime() (time.Duration, error) {
	s, err := m.Get(":GL#")
	if err != nil {
		return 0, err
	}
	h, err := lx200.ParseSexagesimal(strings.TrimSpace(s))
	if err != nil {
		return 0, err
	}
	return time.Duration(h * float64(time.Hour)), nil
}

// UpdateFromGPS commands the mount to take clock/site data from a connected GPS
// module (:gT#) and reports success. From firmware 2.12.6 it returns immediately.
func (m *Mount) UpdateFromGPS() (bool, error) {
	s, err := m.Get(":gT#")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(s) == "1", nil
}

// GPSNMEA returns the last NMEA string from the connected GPS, if any (:gps#).
func (m *Mount) GPSNMEA() (string, error) { return m.Get(":gps#") }

// GPSSynced reports whether the mount clock is kept synchronized to the GPS clock
// (:gtg#).
func (m *Mount) GPSSynced() (bool, error) {
	s, err := m.Get(":gtg#")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(s) == "1", nil
}
