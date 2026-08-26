package rst

import (
	"errors"
	"fmt"
)

// ErrNotImplemented is returned by members this mount cannot support.
var ErrNotImplemented = errors.New("rainbow: not implemented on this mount")

// ErrParked is returned by motion refused because the mount is parked.
var ErrParked = errors.New("rainbow: the mount is parked")

// --- home -------------------------------------------------------------------

// AlpacaCanFindHome reports whether FindHome is supported. It is.
func (m *Mount) AlpacaCanFindHome() bool { return true }

// AlpacaFindHome seeks the mechanical home.
func (m *Mount) AlpacaFindHome() error {
	parked, err := m.AlpacaAtPark()
	if err != nil {
		return err
	}
	if parked {
		return fmt.Errorf("%w: unpark before homing", ErrParked)
	}
	return m.FindHome()
}

// AlpacaAtHome reports that the mount has homed this power-cycle.
func (m *Mount) AlpacaAtHome() (bool, error) {
	slewing, err := m.Slewing()
	if err != nil {
		return false, err
	}
	if slewing {
		return false, nil
	}
	return m.AtHome()
}

// --- park -------------------------------------------------------------------

// AlpacaCanPark reports whether Park is supported. It is.
func (m *Mount) AlpacaCanPark() bool { return true }

// AlpacaPark stows the mount along its polar axis and stops tracking. See Park.
func (m *Mount) AlpacaPark() error {
	parked, err := m.AlpacaAtPark()
	if err != nil {
		return err
	}
	if parked {
		return nil
	}
	return m.Park()
}

// AlpacaAtPark reports whether the mount is parked and stopped.
func (m *Mount) AlpacaAtPark() (bool, error) {
	slewing, err := m.Slewing()
	if err != nil {
		return false, err
	}
	if slewing {
		return false, nil
	}
	m.mu.Lock()
	unparked := m.unparked
	m.mu.Unlock()
	if unparked {
		return false, nil
	}
	return m.AtPark()
}

// AlpacaCanUnpark reports whether Unpark is supported.
func (m *Mount) AlpacaCanUnpark() bool { return true }

// AlpacaUnpark takes the mount out of the parked state and resumes tracking.
func (m *Mount) AlpacaUnpark() error {
	parked, err := m.AlpacaAtPark()
	if err != nil {
		return err
	}
	if !parked {
		return nil
	}
	return m.Unpark()
}

// AlpacaCanSetPark reports whether the park position can be redefined.
func (m *Mount) AlpacaCanSetPark() bool { return false }

// AlpacaSetPark always fails with ErrNotImplemented. See AlpacaCanSetPark.
func (m *Mount) AlpacaSetPark() error {
	return fmt.Errorf("%w: this mount parks at the polar axis, a mechanical stow position", ErrNotImplemented)
}
