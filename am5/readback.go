package am5

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mikefsq/lx200"
)

// What the mount will tell you about itself.
//
// Every command here was seen on the wire (docs/am5-command-set.md) and answered by a real AM5,
// and none of them was implemented: the driver could set a site and a clock but never read one
// back, and could not ask the mount which unit it was. ZWO's own tool polls most of these once a
// second, so they are as well exercised as anything in the protocol.
//
// The asymmetries are the mount's, not ours. Longitude is Meade-reversed on the way in and out;
// the UTC offset is "hours added to local to yield UTC", so it runs opposite to a Go zone offset;
// and the date arrives as MM/DD/YY with no century.

// MAC returns the mount's MAC address (:GMA#), e.g. "f412fa60f72d".
//
// It is the only per-unit identity the AM series exposes. The USB descriptor serial is `123456`
// on the mount this was decoded from — plainly a factory default, and not safe to assume unique —
// so this is what distinguishes one mount from another. It costs an open connection to read,
// which makes it an identity check after connecting rather than a way to find a mount.
func (m *Mount) MAC() (string, error) {
	s, err := m.Get(":GMA#")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(s), nil
}

// SiteLatitude reads the configured latitude in degrees, north positive (:Gt#).
func (m *Mount) SiteLatitude() (float64, error) {
	s, err := m.Get(":Gt#")
	if err != nil {
		return 0, err
	}
	return lx200.ParseSexagesimal(s)
}

// SiteLongitude reads the configured longitude in degrees, EAST positive (:Gg#).
//
// The mount answers in the Meade convention, west positive, which is the reverse of what Alpaca
// and this library use — so the sign is flipped here exactly as SetSiteLongitude flips it on the
// way in. A mount configured for +122°24' west answers `+122*24:58#` and this returns −122.41.
func (m *Mount) SiteLongitude() (float64, error) {
	s, err := m.Get(":Gg#")
	if err != nil {
		return 0, err
	}
	deg, err := lx200.ParseSexagesimal(s)
	if err != nil {
		return 0, err
	}
	return -deg, nil
}

// SetSite sets latitude and longitude in one command (:SMGE<lat>&<lon>#, acks '1').
//
// This is what ZWO's tool sends, where SetSiteLatitude and SetSiteLongitude send INDI's separate
// :St# and :Sg#. Both forms are kept: only this one is confirmed on the wire, and only the
// separate ones satisfy the lx200 SiteSetter interface, which sets one axis at a time.
//
// Longitude is reversed on the way out, as it is everywhere else here.
func (m *Mount) SetSite(latDeg, lonDeg float64) error {
	return must(m.Ack(fmt.Sprintf(":SMGE%s&%s#", dms(latDeg, 2), dms(-lonDeg, 3))))
}

// UTCOffset reads the mount's UTC offset (:GG#) as a Go zone offset — east of UTC positive.
//
// The mount's own convention is the opposite: :GG# answers the hours ADDED TO LOCAL to yield UTC,
// so a mount in UTC−8 answers `+08:00#` and this returns −8h. SetUTC negates in the same
// direction, so the two round-trip.
func (m *Mount) UTCOffset() (time.Duration, error) {
	s, err := m.Get(":GG#")
	if err != nil {
		return 0, err
	}
	h, err := lx200.ParseSexagesimal(s)
	if err != nil {
		return 0, err
	}
	return -time.Duration(h * float64(time.Hour)), nil
}

// LocalDate reads the mount's date (:GC#, MM/DD/YY).
//
// The reply carries no century. Two digits are read as 20YY, which is what every LX200 mount and
// every driver reading one assumes, and what the mount itself does with :SC#.
func (m *Mount) LocalDate() (year int, month time.Month, day int, err error) {
	s, err := m.Get(":GC#")
	if err != nil {
		return 0, 0, 0, err
	}
	f := strings.Split(strings.TrimSuffix(strings.TrimSpace(s), "#"), "/")
	if len(f) != 3 {
		return 0, 0, 0, fmt.Errorf("am5: bad :GC# reply %q", s)
	}
	mm, e1 := strconv.Atoi(f[0])
	dd, e2 := strconv.Atoi(f[1])
	yy, e3 := strconv.Atoi(f[2])
	if e1 != nil || e2 != nil || e3 != nil || mm < 1 || mm > 12 {
		return 0, 0, 0, fmt.Errorf("am5: bad :GC# reply %q", s)
	}
	return 2000 + yy, time.Month(mm), dd, nil
}

// LocalTime reads the mount's time of day (:GL#) as a duration since midnight.
func (m *Mount) LocalTime() (time.Duration, error) {
	s, err := m.Get(":GL#")
	if err != nil {
		return 0, err
	}
	h, err := lx200.ParseSexagesimal(s)
	if err != nil {
		return 0, err
	}
	return time.Duration(h * float64(time.Hour)), nil
}

// Clock reads the mount's date, time and UTC offset as one instant (:GC#, :GL#, :GG#).
//
// It is the read half of SetUTC, and returns the mount's LOCAL time in a fixed zone built from
// its offset — so comparing it with time.Now() tells you whether the mount's clock has drifted,
// while .UTC() gives the absolute instant it believes in.
func (m *Mount) Clock() (time.Time, error) {
	year, month, day, err := m.LocalDate()
	if err != nil {
		return time.Time{}, err
	}
	tod, err := m.LocalTime()
	if err != nil {
		return time.Time{}, err
	}
	off, err := m.UTCOffset()
	if err != nil {
		return time.Time{}, err
	}
	zone := time.FixedZone("mount", int(off/time.Second))
	return time.Date(year, month, day, 0, 0, 0, 0, zone).Add(tod), nil
}

// SetLocalTime sets the mount's time of day (:SL HH:MM:SS#, acks '1').
//
// SetUTC sends this as its third command. It is separate because the mount takes date, offset and
// time of day as three independent settings, and a caller correcting a drifting clock should not
// have to rewrite the date to do it.
func (m *Mount) SetLocalTime(t time.Time) error {
	return must(m.Ack(t.Format(":SL15:04:05#")))
}

// TrackingRateIndex reads the tracking rate as the mount's own index (:GT#).
//
// What the indices mean is NOT established. INDI calls this the tracking mode and reads it as a
// 0-based index; every capture we have answers `0#`, so nothing distinguishes the values yet.
// Returned raw rather than mapped onto lx200.DriveRate, because a guessed mapping would be worse
// than none — the caller can see the number and decide.
func (m *Mount) TrackingRateIndex() (int, error) {
	s, err := m.Get(":GT#")
	if err != nil {
		return 0, err
	}
	s = strings.TrimSuffix(strings.TrimSpace(s), "#")
	i, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("am5: bad :GT# reply %q", s)
	}
	return i, nil
}

// Heavy-duty mode trades slew speed for payload: the mount halves its maximum rate, which it
// reports as the rate itself rather than as a flag.
const (
	heavyDutyOn  = "720"  // :GRl# reply with heavy-duty enabled
	heavyDutyOff = "1440" // ... and disabled
)

// HeavyDuty reports whether heavy-duty mode is enabled (:GRl#).
func (m *Mount) HeavyDuty() (bool, error) {
	s, err := m.Get(":GRl#")
	if err != nil {
		return false, err
	}
	switch strings.TrimSuffix(strings.TrimSpace(s), "#") {
	case heavyDutyOn:
		return true, nil
	case heavyDutyOff:
		return false, nil
	}
	return false, fmt.Errorf("am5: unexpected :GRl# reply %q", s)
}

// SetHeavyDuty enables or disables heavy-duty mode (:SRl720# / :SRl1440#).
//
// The command names the maximum rate rather than a state, which is why this is not :SRl%d# with a
// boolean: 720 and 1440 are the only two values INDI sends and the only two :GRl# returns.
func (m *Mount) SetHeavyDuty(on bool) error {
	v := heavyDutyOff
	if on {
		v = heavyDutyOn
	}
	return m.Blind(":SRl" + v + "#")
}
