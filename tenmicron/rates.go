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

// siderealArcsecPerSec is the mean sidereal rate (15.041"/s); the guide rate is a
// fraction of it.
const siderealArcsecPerSec = 15.041

// GuideRateSidereal returns the guide rate as a fraction of sidereal (the lx200
// GuideRater contract / INDI's unit), converting from the mount's arcsec/s.
func (m *Mount) GuideRateSidereal() (float64, error) {
	r, err := m.GuideRate()
	if err != nil {
		return 0, err
	}
	return r / siderealArcsecPerSec, nil
}

// GuideRateMaxArcsec / GuideRateMinArcsec bound the guide rate (:Rg) in arcsec/s:
// the protocol forbids exceeding sidereal (1.0×), and the GM1000HPS manual gives 0.1×
// sidereal as the minimum adjustable guide speed. SetGuideRate clamps to this band.
const (
	GuideRateMaxArcsec = siderealArcsecPerSec       // 1.0× sidereal ≈ 15.041"/s
	GuideRateMinArcsec = siderealArcsecPerSec * 0.1 // 0.1× sidereal ≈ 1.504"/s
)

// SetGuideRate sets the guide rate in arcsec/s (:RgSS.S#), CLAMPED to the mount's
// supported band [GuideRateMinArcsec, GuideRateMaxArcsec] = [0.1×, 1.0×] sidereal. The
// protocol forbids exceeding sidereal (it would reverse the RA axis during an East
// correction) and :Rg is a no-reply command, so an out-of-range value gets no rejection —
// the clamp also keeps the SS.S wire format from overflowing (>=100"/s would widen it).
// The 0.1× floor is the manual's minimum adjustable guide speed. It applies to the
// :Me/:Mw/:Mn/:Ms guide moves and :Mg pulses once the guiding rate is selected.
func (m *Mount) SetGuideRate(arcsecPerSec float64) error {
	if arcsecPerSec > GuideRateMaxArcsec {
		arcsecPerSec = GuideRateMaxArcsec
	}
	if arcsecPerSec < GuideRateMinArcsec {
		arcsecPerSec = GuideRateMinArcsec
	}
	return m.Blind(fmt.Sprintf(":Rg%04.1f#", arcsecPerSec))
}

// GuiderPortEnabled reports the autoguide-port status (:Gge#) — a single bare
// status byte with no '#' terminator (read with getBoolByte, not Get).
func (m *Mount) GuiderPortEnabled() (bool, error) {
	return m.getBoolByte(":Gge#")
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
