package tenmicron

// HomeState is the homing-search result reported by :h?#.
type HomeState int

const (
	HomeFailed     HomeState = 0 // last home search failed
	HomeFound      HomeState = 1 // home position found
	HomeInProgress HomeState = 2 // home search in progress
)

// FindHome seeks the home position and aligns the mount from the alignment
// information in non-volatile memory (:hF#). Effective only on mounts with homing
// sensors (GM4000QCI / AZ2000QCI); on models without them (e.g. GM1000HPS) it has
// no effect. Poll HomeStatus (or the :D# slew progress) for completion.
func (m *Mount) FindHome() error {
	if err := m.Blind(":hF#"); err != nil {
		return err
	}
	m.invalidate()
	return nil
}

// SeekHomeAndStore seeks the home position and STORES the current alignment and
// encoder values there, in non-volatile memory (:hS#). Like FindHome, effective
// only on sensor-equipped mounts.
func (m *Mount) SeekHomeAndStore() error {
	if err := m.Blind(":hS#"); err != nil {
		return err
	}
	m.invalidate()
	return nil
}

// HomeStatus returns the homing-search state (:h?#).
func (m *Mount) HomeStatus() (HomeState, error) {
	// :h?# replies with a SINGLE status character and no '#' terminator (10Micron
	// spec), so it must be read as one byte — reading until '#' waits out the whole
	// command timeout for a delimiter that never comes (a ~3s stall on every poll,
	// since AtHome is in the DeviceState batch).
	b, err := m.AckByte(":h?#")
	if err != nil {
		return HomeFailed, err
	}
	switch b {
	case '1':
		return HomeFound, nil
	case '2':
		return HomeInProgress, nil
	default:
		return HomeFailed, nil
	}
}

// AtHome reports whether the last homing search found the home position
// (HomeStatus == HomeFound). 10Micron has no "currently at home" query, so this
// reflects the search result. Satisfies lx200.Homer.
func (m *Mount) AtHome() (bool, error) {
	st, err := m.HomeStatus()
	if err != nil {
		return false, err
	}
	return st == HomeFound, nil
}
