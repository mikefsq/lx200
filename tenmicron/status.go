package tenmicron

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mikefsq/lx200"
)

func (m *Mount) getInt(cmd string) (int, error) {
	s, err := m.Get(cmd)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(s))
}

// StatusCode returns the mount status code directly (:Gstat#); see the Gstat*
// constants. (Slewing/Tracking/AtPark read the same code cached from :Ginfo#.)
func (m *Mount) StatusCode() (int, error) { return m.getInt(":Gstat#") }

// TimeToTrackingEnd estimates the time until tracking stops at a horizon/flip
// limit (:Gmte#, minutes).
func (m *Mount) TimeToTrackingEnd() (time.Duration, error) {
	n, err := m.getInt(":Gmte#")
	return time.Duration(n) * time.Minute, err
}

// SlewSettleTime returns the post-slew settle time during which :D#/:GDW# keep
// reporting slewing (:Gstm#, seconds).
func (m *Mount) SlewSettleTime() (time.Duration, error) {
	s, err := m.getFloat(":Gstm#")
	return time.Duration(s * float64(time.Second)), err
}

// HighAltitudeLimit / LowAltitudeLimit return the slew altitude limits in degrees
// (:Gh#/:Go#).
func (m *Mount) HighAltitudeLimit() (float64, error) { return m.getLimitDeg(":Gh#") }
func (m *Mount) LowAltitudeLimit() (float64, error)  { return m.getLimitDeg(":Go#") }

func (m *Mount) getLimitDeg(cmd string) (float64, error) {
	s, err := m.Get(cmd)
	if err != nil {
		return 0, err
	}
	s = strings.TrimSuffix(strings.TrimSpace(s), "*") // low/high precision append '*'
	return strconv.ParseFloat(s, 64)
}

// SetHighAltitudeLimit / SetLowAltitudeLimit set the slew altitude limits in whole
// degrees (:ShsDD#/:SosDD#); the low limit's valid range is −5..+45.
func (m *Mount) SetHighAltitudeLimit(deg int) (bool, error) {
	return m.Ack(fmt.Sprintf(":Shs%+03d#", deg))
}

func (m *Mount) SetLowAltitudeLimit(deg int) (bool, error) {
	return m.Ack(fmt.Sprintf(":Sos%+03d#", deg))
}

// MeridianSide selects which side(s) of the meridian the mount may use.
type MeridianSide int

const (
	MeridianBothSides MeridianSide = 1 // both sides allowed
	MeridianWestOnly  MeridianSide = 2 // only west of meridian (slews end pS=East)
	MeridianEastOnly  MeridianSide = 3 // only east of meridian (slews end pS=West)
)

// MeridianSideBehaviour / SetMeridianSideBehaviour get/set the allowed meridian
// side(s) (:GMF#/:SMFn#). Not applicable to altazimuth mounts.
func (m *Mount) MeridianSideBehaviour() (MeridianSide, error) {
	n, err := m.getInt(":GMF#")
	return MeridianSide(n), err
}

func (m *Mount) SetMeridianSideBehaviour(s MeridianSide) (bool, error) {
	return m.Ack(fmt.Sprintf(":SMF%d#", int(s)))
}

// MeridianTrackLimit / MeridianSlewLimit return the meridian limits in degrees
// (:Glmt#/:Glms#).
func (m *Mount) MeridianTrackLimit() (int, error) { return m.getInt(":Glmt#") }
func (m *Mount) MeridianSlewLimit() (int, error)  { return m.getInt(":Glms#") }

// UnattendedFlip reports the unattended-flip setting (:Guaf#).
func (m *Mount) UnattendedFlip() (bool, error) {
	s, err := m.Get(":Guaf#")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(s) == "1", nil
}

// SetUnattendedFlip enables/disables the unattended meridian flip (:Suaf, no reply).
func (m *Mount) SetUnattendedFlip(on bool) error {
	return m.Blind(fmt.Sprintf(":Suaf%d#", b2i(on)))
}

// Flip performs a meridian/azimuth flip (:FLIP#, replies 1 ok / 0 cannot).
func (m *Mount) Flip() error {
	if err := must(m.Ack(":FLIP#")); err != nil {
		return err
	}
	m.invalidate()
	return nil
}

// DestinationSideOfPier reports which side the mount would slew the selected target
// to (:GTsid#): West, East, or Unknown (no/unreachable target).
func (m *Mount) DestinationSideOfPier() (lx200.PierSide, error) {
	n, err := m.getInt(":GTsid#")
	if err != nil {
		return lx200.PierUnknown, err
	}
	switch n {
	case 2:
		return lx200.PierWest, nil
	case 3:
		return lx200.PierEast, nil
	default:
		return lx200.PierUnknown, nil
	}
}
