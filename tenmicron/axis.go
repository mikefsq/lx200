package tenmicron

import (
	"fmt"
	"strings"
)

// Park is the lx200.Parker capability. It matches the vendor ASCOM driver: on
// firmware ≥ 2.9.9 it parks to the ASCOM-defined position saved via SaveParkPosition
// (:PsX#), falling back to the keypad-defined park (:hP#) when none is saved (reply
// "3") — so a client that never called SaveParkPosition still parks sensibly. Older
// firmware (or unknown firmware, e.g. a directly-constructed Mount) parks straight to
// the keypad position. (AtPark comes from the :Ginfo# Gstat.)
func (m *Mount) Park() error {
	if m.FirmwareAtLeast(2, 9, 9) {
		s, err := m.Get(":PsX#")
		if err != nil {
			return err
		}
		switch strings.TrimSpace(s) {
		case "0", "4": // "4" = the mount was already parked
			m.invalidate()
			return nil
		case "1":
			return fmt.Errorf("gotenmicron: park target below lower limit")
		case "2":
			return fmt.Errorf("gotenmicron: park target above high limit")
		case "3": // no saved park position / cannot: fall back to the keypad park
		default:
			return fmt.Errorf("gotenmicron: :PsX# unexpected reply %q", s)
		}
	}
	if err := m.Blind(":hP#"); err != nil { // keypad-defined park position
		return err
	}
	m.invalidate()
	return nil
}

// Unpark unparks the mount (:PO#), which resumes tracking (lx200.Parker).
func (m *Mount) Unpark() error {
	if err := m.Blind(":PO#"); err != nil {
		return err
	}
	m.invalidate()
	return nil
}

// AtPark reports whether the mount is parked (from the :Ginfo# Gstat).
func (m *Mount) AtPark() (bool, error) { s, err := m.status(); return s.IsParked(), err }

// AxisAnglePrimary reads the current angular position of the RA/azimuth axis in
// degrees (:GaXa#).
func (m *Mount) AxisAnglePrimary() (float64, error) { return m.getFloat(":GaXa#") }

// AxisAngleSecondary reads the current angular position of the Dec/altitude axis in
// degrees (:GaXb#).
func (m *Mount) AxisAngleSecondary() (float64, error) { return m.getFloat(":GaXb#") }

// TargetAxisAnglePrimary reads the target angular position of the RA/azimuth axis
// (:QaXa#).
func (m *Mount) TargetAxisAnglePrimary() (float64, error) { return m.getFloat(":QaXa#") }

// TargetAxisAngleSecondary reads the target angular position of the Dec/altitude axis
// (:QaXb#).
func (m *Mount) TargetAxisAngleSecondary() (float64, error) { return m.getFloat(":QaXb#") }

// SetTargetAxisAnglePrimary sets the target angular position of the RA/azimuth axis in
// degrees (:SaXa…#); reports whether the angle is in range.
func (m *Mount) SetTargetAxisAnglePrimary(deg float64) (bool, error) {
	return m.Ack(fmt.Sprintf(":SaXa%+09.4f#", deg))
}

// SetTargetAxisAngleSecondary sets the target angular position of the Dec/altitude axis
// in degrees (:SaXb…#); reports whether the angle is in range.
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

// SaveParkPosition stores the current angular position as the park position used by
// Park / ParkToSaved (:PyX#). The reply is a SINGLE bare status byte with no '#'
// terminator (read with AckByte, not Get, which would stall the command timeout). The
// spec documents both success and failure as "0", but the vendor ASCOM driver treats
// reply '1' as success and anything else as failure — the mount's actual behaviour —
// so this does the same rather than swallowing a failed save.
func (m *Mount) SaveParkPosition() error {
	b, err := m.AckByte(":PyX#")
	if err != nil {
		return err
	}
	if b != '1' {
		return fmt.Errorf("gotenmicron: :PyX# save-park rejected (reply %q)", string(b))
	}
	m.invalidate()
	return nil
}

// parkCode runs a park/slew command whose reply is a single status digit: "0#" ok /
// "1#" below the lower limit / "2#" above the high limit / "3#" cannot park (mount
// state) / "4#" already parked. It invalidates the cache and returns nil on 0 or 4.
func (m *Mount) parkCode(cmd string) error {
	s, err := m.Get(cmd)
	if err != nil {
		return err
	}
	switch strings.TrimSpace(s) {
	case "0", "4": // "4" = already parked
		m.invalidate()
		return nil
	case "1":
		return fmt.Errorf("gotenmicron: %s target below lower limit", cmd)
	case "2":
		return fmt.Errorf("gotenmicron: %s target above high limit", cmd)
	case "3":
		return fmt.Errorf("gotenmicron: %s cannot park in the current mount state", cmd)
	default:
		return fmt.Errorf("gotenmicron: %s unexpected reply %q", cmd, s)
	}
}
