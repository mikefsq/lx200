package tenmicron

import (
	"fmt"
	"strings"
	"time"

	"github.com/mikefsq/lx200"
)

// SetSiteLatitude sets the observing-site latitude in degrees (:St…#, lx200.SiteSetter).
func (m *Mount) SetSiteLatitude(deg float64) error {
	if err := must(m.Ack(":St" + dms(deg, 2) + "#")); err != nil {
		return err
	}
	m.mu.Lock()
	m.southern = deg < 0 // keep the hemisphere current for pierInverted
	m.mu.Unlock()
	return nil
}

// SetSiteLongitude sets the observing-site longitude in degrees (:Sg…#, lx200.SiteSetter).
// The Alpaca East-positive value is negated because 10Micron expresses East as negative.
func (m *Mount) SetSiteLongitude(deg float64) error {
	return must(m.Ack(":Sg" + dms(-deg, 3) + "#")) // negate: 10Micron East = negative
}

// SetSiteElevation sets the observing-site elevation in metres (:Sev…#, lx200.SiteSetter).
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

// SetLocalDateTime sets the mount's local date and time together (:SLDT…#, firmware ≥
// 2.12.26). t's wall-clock fields are used as-is (the mount applies no zone); with a
// zero UTC offset the local clock equals UTC. Prefer SetUTC for the UTC clock.
func (m *Mount) SetLocalDateTime(t time.Time) error {
	return must(m.Ack(t.Format(":SLDT2006-01-02,15:04:05#")))
}

// NextLeapSecond returns the UTC date of the next scheduled leap second (:GULEAP#,
// firmware ≥ 2.13.1). ok is false when none is scheduled in the mount's data ("E#").
func (m *Mount) NextLeapSecond() (date time.Time, ok bool, err error) {
	s, err := m.Get(":GULEAP#")
	if err != nil {
		return time.Time{}, false, err
	}
	s = strings.TrimSpace(s)
	if s == "E" {
		return time.Time{}, false, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("gotenmicron: bad :GULEAP# date %q", s)
	}
	return t, true, nil
}

// DeltaTStatus reports the validity and expiry date of the mount's ΔT (UT1−UTC) data
// (:GDUTV#, firmware ≥ 2.15): valid is false once the data has expired.
func (m *Mount) DeltaTStatus() (valid bool, expiry time.Time, err error) {
	s, err := m.Get(":GDUTV#")
	if err != nil {
		return false, time.Time{}, err
	}
	f := strings.SplitN(strings.TrimSpace(s), ",", 2)
	if len(f) != 2 {
		return false, time.Time{}, fmt.Errorf("gotenmicron: bad :GDUTV# reply %q", s)
	}
	valid = f[0] == "V" // "V" valid, "E" expired
	expiry, _ = time.Parse("2006-01-02", strings.TrimSpace(f[1]))
	return valid, expiry, nil
}

// GPSMinusUTC returns the current GPS−UTC difference in whole seconds (:GDGPS#,
// firmware ≥ 2.13.1).
func (m *Mount) GPSMinusUTC() (int, error) { return m.getInt(":GDGPS#") }

// SetJulianDate sets the mount clock by Julian Date (computed from UTC) (:SJD…#, up to
// eight decimals). The mount rejects a value during a leap second, when no valid
// Julian Date exists.
func (m *Mount) SetJulianDate(jd float64) error {
	return must(m.Ack(fmt.Sprintf(":SJD%.8f#", jd)))
}

// GPSSync is how tightly the mount clock is kept to a connected GPS module (:gtgpps#).
type GPSSync int

const (
	GPSSyncOff GPSSync = 0 // not synchronized to the GPS clock
	GPSSyncGPS GPSSync = 1 // synchronized to the GPS clock
	GPSSyncPPS GPSSync = 2 // synchronized to the GPS clock and the PPS signal
)

// GPSSyncState reports whether — and how precisely — the mount clock is kept
// synchronized to a connected GPS module (:gtgpps#, firmware ≥ 3.2): off, GPS, or
// GPS+PPS. (The plain boolean form is GPSSynced, via :gtg#.)
func (m *Mount) GPSSyncState() (GPSSync, error) {
	n, err := m.getInt(":gtgpps#")
	return GPSSync(n), err
}

// GPSWeekRollover returns the number of weeks (a multiple of 1024) the mount adds to
// the GPS clock to compensate for the GPS week-rollover (:GGPSRW#, firmware ≥ 3.2.5,
// or ≥ 2.17.1 on the 2.x series).
func (m *Mount) GPSWeekRollover() (int, error) { return m.getInt(":GGPSRW#") }

// ResetGPSWeekRollover resets the GPS week-rollover compensation (:SGPSRW#); the week
// count is recomputed from the current date the next time the GPS clock is received.
// Set the date first (SetDate/SetJulianDate) with the GPS connected — see the spec.
func (m *Mount) ResetGPSWeekRollover() error {
	s, err := m.Get(":SGPSRW#")
	if err != nil {
		return err
	}
	if strings.TrimSpace(s) != "V" {
		return fmt.Errorf("gotenmicron: :SGPSRW# reset failed (%q)", s)
	}
	return nil
}

// NudgeTime adjusts the mount clock by a small amount, ±999 milliseconds (:NUtim…#),
// for fine time synchronization. Errors if the mount rejects the value.
func (m *Mount) NudgeTime(ms int) error {
	if ms < -999 || ms > 999 {
		return fmt.Errorf("gotenmicron: time nudge %d ms outside [-999, 999]", ms)
	}
	s, err := m.Get(fmt.Sprintf(":NUtim%+04d#", ms))
	if err != nil {
		return err
	}
	if strings.TrimSpace(s) != "1" { // "1#" ok, "0#" failed
		return fmt.Errorf("gotenmicron: time nudge rejected (%q)", s)
	}
	return nil
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

// dateTime reads a "<date>,<time>#" reply (:GUDT#/:GLDT#). The date format is
// PRECISION-dependent: in the ultra precision (:U2#) mode Connect forces, the mount
// returns ISO "YYYY-MM-DD"; in low/high precision it is "MM/DD/YY" (or "MM:DD:YY" in
// extended emulation). So the ISO layout is tried first, then the classic ones. The
// time is "HH:MM:SS[.ss]"; Go's time.Parse accepts the optional fractional part even
// when the layout omits it.
func (m *Mount) dateTime(cmd string) (time.Time, error) {
	s, err := m.Get(cmd)
	if err != nil {
		return time.Time{}, err
	}
	s = strings.TrimSpace(s)
	for _, layout := range []string{
		"2006-01-02,15:04:05", // ultra precision (:U2#) — the Connect default
		"01/02/06,15:04:05",
		"01:02:06,15:04:05",
	} {
		if t, e := time.Parse(layout, s); e == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("gotenmicron: unrecognized %s date/time %q", cmd, s)
}

// LocalDate reads the mount's current local date (:GC#). The reply format is
// precision-dependent: ISO "YYYY-MM-DD" in the ultra (:U2#) mode Connect forces, else
// "MM/DD/YY" (or "MM:DD:YY" in extended emulation). All are parsed. Time fields zero.
func (m *Mount) LocalDate() (time.Time, error) {
	s, err := m.Get(":GC#")
	if err != nil {
		return time.Time{}, err
	}
	s = strings.TrimSpace(s)
	for _, layout := range []string{"2006-01-02", "01/02/06", "01:02:06"} {
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
