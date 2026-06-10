package tenmicron

import (
	"fmt"
	"strings"
)

// Park / Unpark / AtPark are the lx200.Parker capability (:KA# / :PO#, no reply;
// AtPark from the :Ginfo# Gstat). Park sends :KA#; the spec's :hP# is a byte-for-
// byte alias ("slew to park position"), so it needs no separate method.
func (m *Mount) Park() error {
	if err := m.Blind(":KA#"); err != nil {
		return err
	}
	m.invalidate()
	return nil
}

func (m *Mount) Unpark() error {
	if err := m.Blind(":PO#"); err != nil {
		return err
	}
	m.invalidate()
	return nil
}

func (m *Mount) AtPark() (bool, error) { s, err := m.status(); return s.IsParked(), err }

// AxisAnglePrimary / AxisAngleSecondary read the current angular position of the
// RA/azimuth (a) and Dec/altitude (b) axes in degrees (:GaXa#/:GaXb#).
func (m *Mount) AxisAnglePrimary() (float64, error)   { return m.getFloat(":GaXa#") }
func (m *Mount) AxisAngleSecondary() (float64, error) { return m.getFloat(":GaXb#") }

// TargetAxisAnglePrimary / TargetAxisAngleSecondary read the target angular
// positions (:QaXa#/:QaXb#).
func (m *Mount) TargetAxisAnglePrimary() (float64, error)   { return m.getFloat(":QaXa#") }
func (m *Mount) TargetAxisAngleSecondary() (float64, error) { return m.getFloat(":QaXb#") }

// SetTargetAxisAnglePrimary / SetTargetAxisAngleSecondary set the target angular
// positions in degrees (:SaXa…#/:SaXb…#); report whether the angle is in range.
func (m *Mount) SetTargetAxisAnglePrimary(deg float64) (bool, error) {
	return m.Ack(fmt.Sprintf(":SaXa%+09.4f#", deg))
}

func (m *Mount) SetTargetAxisAngleSecondary(deg float64) (bool, error) {
	return m.Ack(fmt.Sprintf(":SaXb%+09.4f#", deg))
}

// SlewToAxisTarget slews to the angular targets set by SetTargetAxisAngle* and
// stops (:MaX#). Uses the LX200 slew reply shape.
func (m *Mount) SlewToAxisTarget() error { return m.slewInvalidate(":MaX#") }

// SlewToAxisTargetAndPark slews to the angular targets and parks (:PaX#).
func (m *Mount) SlewToAxisTargetAndPark() error { return m.parkCode(":PaX#") }

// ParkInPlace parks the mount at its current position (:PiP#).
func (m *Mount) ParkInPlace() error {
	s, err := m.Get(":PiP#")
	if err != nil {
		return err
	}
	if strings.TrimSpace(s) != "1" { // "1#" parked, "0#" error
		return fmt.Errorf("gotenmicron: park-in-place failed (%q)", s)
	}
	m.invalidate()
	return nil
}

// ParkToSaved slews to the saved park angular position and parks (:PsX#).
func (m *Mount) ParkToSaved() error { return m.parkCode(":PsX#") }

// SaveParkPosition stores the current angular position as the park position used
// by ParkToSaved (:PyX#). The spec's reply codes are ambiguous (both documented as
// "0"), so only a transport error is reported.
func (m *Mount) SaveParkPosition() error {
	_, err := m.Get(":PyX#")
	if err == nil {
		m.invalidate()
	}
	return err
}

// parkCode runs a park/slew command whose reply is "0#" (ok) / "1#" (below low
// limit) / "2#" (above high limit) and invalidates the cache on success.
func (m *Mount) parkCode(cmd string) error {
	s, err := m.Get(cmd)
	if err != nil {
		return err
	}
	switch strings.TrimSpace(s) {
	case "0":
		m.invalidate()
		return nil
	case "1":
		return fmt.Errorf("gotenmicron: %s target below lower limit", cmd)
	case "2":
		return fmt.Errorf("gotenmicron: %s target above high limit", cmd)
	default:
		return fmt.Errorf("gotenmicron: %s unexpected reply %q", cmd, s)
	}
}
