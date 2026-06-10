package tenmicron

import (
	"fmt"
	"strconv"
	"strings"
)

// SetTracking starts (:AP#) or stops (:AL#) tracking. Both reply nothing.
func (m *Mount) SetTracking(on bool) error {
	cmd := ":AL#"
	if on {
		cmd = ":AP#"
	}
	if err := m.Blind(cmd); err != nil {
		return err
	}
	m.invalidate()
	return nil
}

// SetDualAxisTracking enables/disables dual-axis tracking (:Sdat, replies 0/1).
func (m *Mount) SetDualAxisTracking(on bool) error {
	return must(m.Ack(fmt.Sprintf(":Sdat%d#", b2i(on))))
}

// TrackRate selects a tracking rate via the 10Micron :RT family.
type TrackRate int

const (
	TrackLunarRate    TrackRate = 0 // :RT0#
	TrackSolarRate    TrackRate = 1 // :RT1#
	TrackSiderealRate TrackRate = 2 // :RT2#
	TrackStopped      TrackRate = 9 // :RT9# — stop tracking
)

// SetTrackRate selects a tracking rate (:RT0/1/2/9#, no reply).
func (m *Mount) SetTrackRate(r TrackRate) error {
	if err := m.Blind(fmt.Sprintf(":RT%d#", int(r))); err != nil {
		return err
	}
	m.invalidate()
	return nil
}

// TrackSidereal / TrackLunar / TrackSolar override the core lx200.TrackRater so
// this dialect uses the documented :RT family. The core's TrackSolar emits :TS#,
// which is NOT a 10Micron command — hence this override is required, not cosmetic.
func (m *Mount) TrackSidereal() error { return m.SetTrackRate(TrackSiderealRate) }
func (m *Mount) TrackLunar() error    { return m.SetTrackRate(TrackLunarRate) }
func (m *Mount) TrackSolar() error    { return m.SetTrackRate(TrackSolarRate) }

// SelectCustomTrackRate switches the mount to the custom tracking rate (:TM#);
// adjust it with IncCustomTrackRate / DecCustomTrackRate.
func (m *Mount) SelectCustomTrackRate() error {
	if err := m.Blind(":TM#"); err != nil {
		return err
	}
	m.invalidate()
	return nil
}

// SetCustomRARate sets the right-ascension custom tracking rate as a multiple of
// the sidereal speed, added to standard sidereal tracking (:RRsXXX.XXXX#). 0 = pure
// sidereal. (10Micron has no read-back for this rate.)
func (m *Mount) SetCustomRARate(multiplesOfSidereal float64) error {
	return must(m.Ack(fmt.Sprintf(":RR%+09.4f#", multiplesOfSidereal)))
}

// SetCustomDecRate sets the declination custom tracking rate as a multiple of the
// sidereal speed (:RDsXXX.XXXX#). 0 = no declination drift.
func (m *Mount) SetCustomDecRate(multiplesOfSidereal float64) error {
	return must(m.Ack(fmt.Sprintf(":RD%+09.4f#", multiplesOfSidereal)))
}

// IncCustomTrackRate raises the custom tracking rate by 0.025 arcsec/s (:T+#).
func (m *Mount) IncCustomTrackRate() error { return m.Blind(":T+#") }

// DecCustomTrackRate lowers the custom tracking rate by 0.025 arcsec/s (:T-#).
func (m *Mount) DecCustomTrackRate() error { return m.Blind(":T-#") }

// TrackingRateHz returns the tracking rate as the emulated LX200 value (:GT#,
// "TT.T"): the equivalent motor frequency in Hz (60.0 Hz ≈ sidereal).
func (m *Mount) TrackingRateHz() (float64, error) {
	s, err := m.Get(":GT#")
	if err != nil {
		return 0, err
	}
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}

// TrackingActive reports the mount's live tracking state (:GTRK#), queried
// directly rather than derived from the cached :Ginfo status.
func (m *Mount) TrackingActive() (bool, error) {
	s, err := m.Get(":GTRK#")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(s) == "1", nil
}
