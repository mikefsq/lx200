package rst

import (
	"fmt"
	"strings"
)

// Satellite tracking (:V). Every command is write only.

// MaxSatellites is the mount's satellite table size.
const MaxSatellites = 14

// TLE orbital elements.
type Satellite struct {
	Name              string // :VN
	EpochYear         string // :VE, 2 digits, joined with EpochDay
	EpochDay          string // :VE and :VP, day-of-year then the fractional day
	FirstDerivative   string // :Vd, 8 digits
	Inclination       string // :Vi
	RightAscension    string // :Vo, RAAN
	Eccentricity      string // :Ve, sent as 0.NNN
	ArgumentOfPerigee string // :VO
	MeanAnomaly       string // :VM
	MeanMotion        string // :VR
	Magnitude         string // :Vm
}

// UploadSatellite writes one satellite into slot.
func (m *Mount) UploadSatellite(slot int, s Satellite) error {
	if slot < 0 || slot >= MaxSatellites {
		return fmt.Errorf("rainbow: satellite slot %d out of range 0..%d", slot, MaxSatellites-1)
	}
	steps := []string{
		fmt.Sprintf(":Vn%02d#", slot),
		":VE" + s.EpochYear + s.EpochDay + "#",
		":VP" + s.EpochDay + "#",
		":Vd" + s.FirstDerivative + "#",
		":Vi" + s.Inclination + "#",
		":Vo" + s.RightAscension + "#",
		":Ve" + strings.TrimPrefix(s.Eccentricity, "0.") + "#",
		":VO" + s.ArgumentOfPerigee + "#",
		":VM" + s.MeanAnomaly + "#",
		":VR" + s.MeanMotion + "#",
		":VN" + s.Name + "#",
		":Vm" + s.Magnitude + "#",
		fmt.Sprintf(":VA%02d#", slot),
	}
	for _, cmd := range steps {
		if err := m.Blind(cmd); err != nil {
			return fmt.Errorf("rainbow: satellite upload %s: %w", cmd, err)
		}
	}
	return nil
}

// SelectSatelliteSlot selects a slot without writing elements (:Vn#).
func (m *Mount) SelectSatelliteSlot(slot int) error {
	if slot < 0 || slot >= MaxSatellites {
		return fmt.Errorf("rainbow: satellite slot %d out of range 0..%d", slot, MaxSatellites-1)
	}
	return m.Blind(fmt.Sprintf(":Vn%02d#", slot))
}

// CommitSatellite sends the command the vendor tool sends last for each slot (:VA#).
func (m *Mount) CommitSatellite(slot int) error {
	if slot < 0 || slot >= MaxSatellites {
		return fmt.Errorf("rainbow: satellite slot %d out of range 0..%d", slot, MaxSatellites-1)
	}
	return m.Blind(fmt.Sprintf(":VA%02d#", slot))
}

// SatelliteValueB sends :VB# with a two-digit argument.
func (m *Mount) SatelliteValueB(v int) error { return m.Blind(fmt.Sprintf(":VB%02d#", v)) }

// SatelliteValueC sends :VC# with a two-digit argument.
func (m *Mount) SatelliteValueC(v int) error { return m.Blind(fmt.Sprintf(":VC%02d#", v)) }

// SatelliteValueD sends :VD# with a two-digit argument.
func (m *Mount) SatelliteValueD(v int) error { return m.Blind(fmt.Sprintf(":VD%02d#", v)) }

// SatelliteValueF sends :VF# with a one-digit argument.
func (m *Mount) SatelliteValueF(v int) error { return m.Blind(fmt.Sprintf(":VF%d#", v)) }

// SatelliteValueLowerB sends :Vb# with an eight-digit argument.
func (m *Mount) SatelliteValueLowerB(v int) error {
	return m.Blind(fmt.Sprintf(":Vb%08d#", v))
}

// SatelliteFlag sends one of the :V commands that take no argument (T, t, U, u, V).
func (m *Mount) SatelliteFlag(c byte) error {
	switch c {
	case 'T', 't', 'U', 'u', 'V':
		return m.Blind(fmt.Sprintf(":V%c#", c))
	}
	return fmt.Errorf("rainbow: :V%c is not one of the argument-less satellite flags (T t U u V)", c)
}
