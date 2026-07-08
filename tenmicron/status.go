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

// TargetTrackable reports whether the currently set target sits where tracking is
// allowed (:GTTRK#): false when it is below the horizon, or above +89° on an
// altazimuth mount. The reply is a single status byte with no '#' terminator.
func (m *Mount) TargetTrackable() (bool, error) { return m.getBoolByte(":GTTRK#") }

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

// SetSlewSettleTime sets the post-slew settle time (:Sstm…#, 0..99999 s): after a slew
// completes, :D#/:GDW# report slewing for this duration (:Gstat# is unaffected).
func (m *Mount) SetSlewSettleTime(d time.Duration) error { return m.setSettle(":Sstm", d) }

// setSettle formats a settle-time duration as the mount's NNNNN.NNN-second field and
// sends it as an ack command (:Sstm / :SDstm), rejecting values outside 0..99999 s.
func (m *Mount) setSettle(cmd string, d time.Duration) error {
	secs := d.Seconds()
	if secs < 0 || secs > 99999 {
		return fmt.Errorf("gotenmicron: settle time %.3fs outside [0, 99999]", secs)
	}
	return must(m.Ack(fmt.Sprintf("%s%09.3f#", cmd, secs)))
}

// HighAltitudeLimit returns the upper slew altitude limit in degrees (:Gh#).
func (m *Mount) HighAltitudeLimit() (float64, error) { return m.getLimitDeg(":Gh#") }

// LowAltitudeLimit returns the lower slew altitude limit in degrees (:Go#).
func (m *Mount) LowAltitudeLimit() (float64, error) { return m.getLimitDeg(":Go#") }

func (m *Mount) getLimitDeg(cmd string) (float64, error) {
	s, err := m.Get(cmd)
	if err != nil {
		return 0, err
	}
	s = strings.TrimSuffix(strings.TrimSpace(s), "*") // low/high precision append '*'
	return strconv.ParseFloat(s, 64)
}

// SetHighAltitudeLimit sets the upper slew altitude limit in whole degrees (:Sh…#);
// reports whether the mount accepted it.
func (m *Mount) SetHighAltitudeLimit(deg int) (bool, error) {
	return m.Ack(fmt.Sprintf(":Sh%+03d#", deg))
}

// SetLowAltitudeLimit sets the lower slew altitude limit in whole degrees (:So…#, valid
// range −5..+45); reports whether the mount accepted it.
func (m *Mount) SetLowAltitudeLimit(deg int) (bool, error) {
	return m.Ack(fmt.Sprintf(":So%+03d#", deg))
}

// MeridianSide selects which side(s) of the meridian the mount may use.
type MeridianSide int

const (
	MeridianBothSides MeridianSide = 1 // both sides allowed
	MeridianWestOnly  MeridianSide = 2 // only west of meridian (slews end pS=East)
	MeridianEastOnly  MeridianSide = 3 // only east of meridian (slews end pS=West)
)

// MeridianSideBehaviour reads the allowed meridian side(s) (:GMF#). Not applicable to
// altazimuth mounts.
func (m *Mount) MeridianSideBehaviour() (MeridianSide, error) {
	n, err := m.getInt(":GMF#")
	return MeridianSide(n), err
}

// SetMeridianSideBehaviour sets the allowed meridian side(s) (:SMFn#); reports whether
// the mount accepted it. Not applicable to altazimuth mounts.
func (m *Mount) SetMeridianSideBehaviour(s MeridianSide) (bool, error) {
	return m.Ack(fmt.Sprintf(":SMF%d#", int(s)))
}

// MeridianTrackLimit returns the meridian tracking limit in degrees (:Glmt#).
func (m *Mount) MeridianTrackLimit() (int, error) { return m.getInt(":Glmt#") }

// MeridianSlewLimit returns the meridian slew limit in degrees (:Glms#).
func (m *Mount) MeridianSlewLimit() (int, error) { return m.getInt(":Glms#") }

// SetMeridianTrackLimit sets the meridian limit for tracking in degrees (:Slmt#); its
// minimum is the slew meridian limit. Reports acceptance. Not settable on AZ2000/4000HPS
// (the mount returns false there). (Firmware ≥ 2.11.)
func (m *Mount) SetMeridianTrackLimit(deg int) (bool, error) {
	return m.Ack(fmt.Sprintf(":Slmt%d#", deg))
}

// SetMeridianSlewLimit sets the meridian limit for slews in degrees (:Slms#); a value
// above the tracking limit raises the tracking limit to match. Reports acceptance. Not
// settable on AZ2000/4000HPS. (Firmware ≥ 2.11.)
func (m *Mount) SetMeridianSlewLimit(deg int) (bool, error) {
	return m.Ack(fmt.Sprintf(":Slms%d#", deg))
}

// UnattendedFlip reports the unattended-flip setting (:Guaf#). Like :h?#, the reply
// is a single status character with no '#' terminator, so it must be read as one
// byte — reading until '#' stalls for the whole command timeout (see HomeStatus).
func (m *Mount) UnattendedFlip() (bool, error) {
	b, err := m.AckByte(":Guaf#")
	if err != nil {
		return false, err
	}
	return b == '1', nil
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
// to (:GTsid#): West, East, or Unknown (no/unreachable target). :GTsid# replies a
// SINGLE bare status digit with no '#' terminator (read with AckByte — reading until
// '#' via getInt would stall the command timeout).
func (m *Mount) DestinationSideOfPier() (lx200.PierSide, error) {
	b, err := m.AckByte(":GTsid#")
	if err != nil {
		return lx200.PierUnknown, err
	}
	var p lx200.PierSide
	switch b {
	case '2':
		p = lx200.PierWest
	case '3':
		p = lx200.PierEast
	default:
		return lx200.PierUnknown, nil
	}
	if m.pierInverted() { // southern-hemisphere correction on old firmware (see pierInverted)
		p = invertPier(p)
	}
	return p, nil
}
