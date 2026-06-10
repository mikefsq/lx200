package tenmicron

import (
	"fmt"
	"strconv"
	"strings"
)

// SetGuidingRateIndex sets the guiding-rate step (:RGn#): 0=0.25×, 1=0.5×, 2=1.0×
// sidereal. (Distinct from the core SetRate(:RG#) preset selector.)
func (m *Mount) SetGuidingRateIndex(n int) error { return m.Blind(fmt.Sprintf(":RG%d#", n)) }

// SetCenteringRateIndex sets the centering-rate step (:RCn#): 0=16×, 1=64×,
// 2=600×, 3=1200×.
func (m *Mount) SetCenteringRateIndex(n int) error { return m.Blind(fmt.Sprintf(":RC%d#", n)) }

// SetSlewRateIndex sets the slew-rate step (:RSn#): 0=1200×, 1=900×, 2=600×.
func (m *Mount) SetSlewRateIndex(n int) error { return m.Blind(fmt.Sprintf(":RS%d#", n)) }

// SetMaxSlewRate sets the maximum slew rate in degrees/s (:SwN#). Reports whether
// the mount accepted the value.
func (m *Mount) SetMaxSlewRate(degPerSec int) (bool, error) {
	return m.Ack(fmt.Sprintf(":Sw%d#", degPerSec))
}

// SlewRate / MinSlewRate / MaxSlewRate read the current / minimum / maximum slew
// rate in degrees/s (:GMs#/:GMsa#/:GMsb#).
func (m *Mount) SlewRate() (float64, error)    { return m.getFloat(":GMs#") }
func (m *Mount) MinSlewRate() (float64, error) { return m.getFloat(":GMsa#") }
func (m *Mount) MaxSlewRate() (float64, error) { return m.getFloat(":GMsb#") }

// GuideRate returns the current guide rate in arcsec/s (:Ggui#).
func (m *Mount) GuideRate() (float64, error) { return m.getFloat(":Ggui#") }

// SetGuideRate sets the guide rate in arcsec/s (:RgSS.S#); must not exceed the
// sidereal rate (~15.041"/s). It is applied to the :Me/:Mw/:Mn/:Ms guide moves
// once guiding rate is selected.
func (m *Mount) SetGuideRate(arcsecPerSec float64) error {
	return m.Blind(fmt.Sprintf(":Rg%04.1f#", arcsecPerSec))
}

// GuiderPortEnabled reports the autoguide-port status (:Gge#).
func (m *Mount) GuiderPortEnabled() (bool, error) {
	s, err := m.Get(":Gge#")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(s) == "1", nil
}

// SetGuiderPortEnabled enables/disables the autoguide port (:SgeN#).
func (m *Mount) SetGuiderPortEnabled(on bool) error {
	return m.Blind(fmt.Sprintf(":Sge%d#", b2i(on)))
}

// GuidingState reports the current guiding status (:Gpgc#): 0 not guiding,
// 1 RA/Az, 2 Dec/Alt (a per-axis code per the spec).
func (m *Mount) GuidingState() (int, error) {
	s, err := m.Get(":Gpgc#")
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(s))
}

// getFloat reads a numeric '#'-terminated reply and parses it as a float.
func (m *Mount) getFloat(cmd string) (float64, error) {
	s, err := m.Get(cmd)
	if err != nil {
		return 0, err
	}
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}
