package tenmicron

import (
	"fmt"
	"math"

	"github.com/mikefsq/lx200"
)

// SetTargetAltitude sets the target object altitude in degrees (:Sa…#). Reports
// whether the target is within the slew range (mount reply 1 = in range).
func (m *Mount) SetTargetAltitude(deg float64) (bool, error) {
	return m.Ack(":Sa" + dms(deg, 2) + "#")
}

// SetTargetAzimuth sets the target object azimuth in degrees, 0..360 (:Sz…#).
// Reports whether the mount accepted it (reply 1 = valid).
func (m *Mount) SetTargetAzimuth(deg float64) (bool, error) {
	return m.Ack(":Sz" + dmsUnsigned(deg, 3) + "#")
}

// SlewToAltAz sets the alt/az target and slews to it (:Sa, :Sz, :MA#). :MA# uses
// the LX200 slew reply shape ('0' = started, else a '#'-terminated fault).
func (m *Mount) SlewToAltAz(altDeg, azDeg float64) error {
	if err := m.setAltAzTarget(altDeg, azDeg); err != nil {
		return err
	}
	if err := m.Slew(":MA#"); err != nil {
		return err
	}
	m.invalidate()
	return nil
}

// SyncToAltAz sets the alt/az target and syncs the mount's position to it
// (:Sa, :Sz, :CM#), returning the mount's sync reply.
func (m *Mount) SyncToAltAz(altDeg, azDeg float64) (string, error) {
	if err := m.setAltAzTarget(altDeg, azDeg); err != nil {
		return "", err
	}
	return m.SyncToTarget()
}

func (m *Mount) setAltAzTarget(altDeg, azDeg float64) error {
	if ok, err := m.SetTargetAltitude(altDeg); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("gotenmicron: target altitude %.4f out of slew range", altDeg)
	}
	if ok, err := m.SetTargetAzimuth(azDeg); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("gotenmicron: target azimuth %.4f invalid", azDeg)
	}
	return nil
}

// TargetRA / TargetDec / TargetAltitude / TargetAzimuth read the currently set
// target coordinates (:Gr#/:Gd#/:Ga#/:Gz#). RA in hours; the rest in degrees.
func (m *Mount) TargetRA() (float64, error)       { return m.getAngle(":Gr#") }
func (m *Mount) TargetDec() (float64, error)      { return m.getAngle(":Gd#") }
func (m *Mount) TargetAltitude() (float64, error) { return m.getAngle(":Ga#") }
func (m *Mount) TargetAzimuth() (float64, error)  { return m.getAngle(":Gz#") }

func (m *Mount) getAngle(cmd string) (float64, error) {
	s, err := m.Get(cmd)
	if err != nil {
		return 0, err
	}
	return lx200.ParseSexagesimal(s)
}

// dmsUnsigned formats unsigned degrees as "DDD*MM:SS" with a fixed degree-field
// width, rounded to the nearest arcsecond (for azimuth, which has no sign).
func dmsUnsigned(deg float64, degWidth int) string {
	t := int(math.Round(deg * 3600))
	return fmt.Sprintf("%0*d*%02d:%02d", degWidth, t/3600, (t/60)%60, t%60)
}
