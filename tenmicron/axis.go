package tenmicron

import (
	"errors"
	"fmt"
	"strconv"
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

// SlewToHome sends the mount to its mechanical home and PARKS there (:hP#) — the
// keypad-defined park position, which on a polar-aligned German mount leaves the OTA
// pointing up the RA axis (primary axis ≈ 90°, secondary ≈ 0°). It is the same command
// Park falls back to when no ASCOM park is saved; :hP# returns no reply, so it is Blind.
// AtPark reads true afterwards, and the axis angles (AxisAnglePrimary/Secondary) report
// the RA-pole position — the basis for a driver's "at home" indication.
func (m *Mount) SlewToHome() error {
	if err := m.Blind(":hP#"); err != nil {
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

// ErrNoAxisTarget is returned by TargetAxisAngle{Primary,Secondary} when no axis
// target is set (the mount replies "E#") — e.g. the last goto was a coordinate
// (RA/Dec or alt/az) slew rather than an axis-angle target set via
// SetTargetAxisAngle*.
var ErrNoAxisTarget = errors.New("gotenmicron: no axis target set")

// TargetAxisAnglePrimary reads the target angular position of the RA/azimuth axis in
// degrees (:QaXa#), or ErrNoAxisTarget if none is set.
func (m *Mount) TargetAxisAnglePrimary() (float64, error) { return m.axisTarget(":QaXa#") }

// TargetAxisAngleSecondary reads the target angular position of the Dec/altitude axis
// in degrees (:QaXb#), or ErrNoAxisTarget if none is set.
func (m *Mount) TargetAxisAngleSecondary() (float64, error) { return m.axisTarget(":QaXb#") }

// axisTarget reads a :QaX{a,b}# target-axis-angle reply as a float, mapping the "E#"
// no-target reply to ErrNoAxisTarget instead of a raw parse error.
func (m *Mount) axisTarget(cmd string) (float64, error) {
	s, err := m.Get(cmd)
	if err != nil {
		return 0, err
	}
	if s = strings.TrimSpace(s); s == "E" {
		return 0, ErrNoAxisTarget
	}
	return strconv.ParseFloat(s, 64)
}

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

// SlewToRAAxis slews to the RA-axis reference — the RA/azimuth axis at 90° and the
// Dec/altitude axis at 0°, so the OTA lies along the polar (RA) axis — and STOPS there
// (:SaXa#/:SaXb#/:MaX#). It does NOT park: the mount is left stopped at a deterministic
// MECHANICAL position (axis-angle targets bypass the pointing model, so it lands on the
// same physical position regardless of the loaded model), rather than in the parked
// state that Park/PaX enters. A home replacement on a mount with no home sensor.
func (m *Mount) SlewToRAAxis() error {
	if err := must(m.SetTargetAxisAnglePrimary(90)); err != nil {
		return err
	}
	if err := must(m.SetTargetAxisAngleSecondary(0)); err != nil {
		return err
	}
	return m.SlewToAxisTarget()
}

// RotateRAAxis slews the RA/azimuth axis to the given mechanical angle in degrees,
// leaving the Dec/altitude axis where it is (:SaXa#/:SaXb#/:MaX#). Like all axis-angle
// commands it targets the raw mechanical position and is independent of the pointing
// model — e.g. to rotate about the RA axis after SlewToRAAxisAndPark.
func (m *Mount) RotateRAAxis(deg float64) error {
	sec, err := m.AxisAngleSecondary()
	if err != nil {
		return err
	}
	if err := must(m.SetTargetAxisAnglePrimary(deg)); err != nil {
		return err
	}
	if err := must(m.SetTargetAxisAngleSecondary(sec)); err != nil {
		return err
	}
	return m.SlewToAxisTarget()
}

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
