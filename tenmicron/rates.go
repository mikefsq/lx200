package tenmicron

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mikefsq/lx200"
)

// SetGuidingRateIndex sets the guiding-rate step (:RGn#): 0=0.25×, 1=0.5×, 2=1.0×
// sidereal. (Distinct from the core SetRate(:RG#) preset selector.)
func (m *Mount) SetGuidingRateIndex(n int) error { return m.Blind(fmt.Sprintf(":RG%d#", n)) }

// SetCenteringRateIndex sets the centering-rate step (:RCn#): 0=16×, 1=64×,
// 2=600×, 3=1200×.
func (m *Mount) SetCenteringRateIndex(n int) error { return m.Blind(fmt.Sprintf(":RC%d#", n)) }

// SetSlewRateIndex sets the slew-rate step (:RSn#): 0=1200×, 1=900×, 2=600×.
func (m *Mount) SetSlewRateIndex(n int) error { return m.Blind(fmt.Sprintf(":RS%d#", n)) }

// SetCenteringRateSidereal sets the centering-rate (:RC) manual-slew speed to x times
// the sidereal rate (:RcXXX#, x = 1..255) — the continuous form of SetCenteringRateIndex.
func (m *Mount) SetCenteringRateSidereal(x int) error {
	if x < 1 || x > 255 {
		return fmt.Errorf("gotenmicron: centering rate %d× outside [1, 255]", x)
	}
	return m.Blind(fmt.Sprintf(":Rc%d#", x))
}

// SetSlewRateSidereal sets the slew-rate (:RS) manual-slew speed to x times the sidereal
// rate (:RsXXXX#, x = 1..1200) — the continuous form of SetSlewRateIndex.
func (m *Mount) SetSlewRateSidereal(x int) error {
	if x < 1 || x > 1200 {
		return fmt.Errorf("gotenmicron: slew rate %d× outside [1, 1200]", x)
	}
	return m.Blind(fmt.Sprintf(":Rs%d#", x))
}

// SetMaxSlewRate sets the maximum slew rate in degrees/s (:SwN#). Reports whether
// the mount accepted the value.
func (m *Mount) SetMaxSlewRate(degPerSec int) (bool, error) {
	return m.Ack(fmt.Sprintf(":Sw%d#", degPerSec))
}

// SetAutomatedSlewRate sets the slew rate for automated (goto) moves to degPerSec
// degrees/second, within the mount's allowed range (:RMs#). It does NOT change the
// manual axis-move rate — follow it with SetSlewRateIndex/SetMaxSlewRate for that.
// (Firmware ≥ 2.9.9.) Note: :RMs is inverted — the mount replies '0' for a valid rate.
func (m *Mount) SetAutomatedSlewRate(degPerSec int) error {
	b, err := m.AckByte(fmt.Sprintf(":RMs%d#", degPerSec))
	if err != nil {
		return err
	}
	if b != '0' { // '0' = valid, '1' = invalid (opposite of the usual ack convention)
		return fmt.Errorf("gotenmicron: automated slew rate %d deg/s rejected (reply %q)", degPerSec, string(b))
	}
	return nil
}

// SlewRate reads the current slew rate in degrees/s (:GMs#).
func (m *Mount) SlewRate() (float64, error) { return m.getFloat(":GMs#") }

// MinSlewRate reads the minimum settable slew rate in degrees/s (:GMsa#).
func (m *Mount) MinSlewRate() (float64, error) { return m.getFloat(":GMsa#") }

// MaxSlewRate reads the maximum settable slew rate in degrees/s (:GMsb#).
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

// GuideRateMaxArcsec is the sidereal rate (≈15.041"/s), the spec's stated ceiling for
// the guide rate ("shall not exceed sidereal speed"). The mount enforces it;
// SetGuideRate does not clamp.
const GuideRateMaxArcsec = siderealArcsecPerSec // 1.0× sidereal ≈ 15.041"/s

// SetGuideRate sets the guide rate in arcsec/s (:Rg00.000#, three decimals to match
// the vendor driver's precision — the spec's SS.S is only its documented minimum). The
// value is sent VERBATIM, without the earlier silent clamp: the mount applies its own
// limit (the spec caps the rate at sidereal, ≈15.041"/s), just as the vendor driver
// leaves it to the mount. It errors only on a value the two-integer-digit :Rg field
// cannot represent (negative or ≥ 100). Applies to the :Me/:Mw/:Mn/:Ms guide moves and
// :Mg pulses once the guiding rate is selected.
func (m *Mount) SetGuideRate(arcsecPerSec float64) error {
	if arcsecPerSec < 0 || arcsecPerSec >= 100 {
		return fmt.Errorf("gotenmicron: guide rate %.3f\"/s outside the :Rg field range [0, 100)", arcsecPerSec)
	}
	return m.Blind(fmt.Sprintf(":Rg%06.3f#", arcsecPerSec))
}

// PulseGuide issues a timed guide pulse of ms milliseconds in a cardinal direction
// (:Mg{n,s,e,w}#; up to 9999 ms on firmware ≥ 2.10). It overrides the core PulseGuide
// only to record the pulse locally so IsPulseGuiding can report it — the mount times
// the pulse itself (fire-and-forget) and replies nothing.
func (m *Mount) PulseGuide(d lx200.Direction, ms int) error {
	if err := m.Conn.PulseGuide(d, ms); err != nil {
		return err
	}
	m.mu.Lock()
	if end := time.Now().Add(time.Duration(ms) * time.Millisecond); end.After(m.pulseUntil) {
		m.pulseUntil = end
	}
	m.mu.Unlock()
	return nil
}

// IsPulseGuiding reports whether a guide pulse issued through PulseGuide is still in
// progress. The mount times each :Mg pulse itself, so this tracks the latest pulse's
// expected end locally — true until the longest outstanding pulse completes, so
// simultaneous N/S and E/W pulses are both covered. For the mount's own view of any
// guiding (including keypad- or other-client-initiated pulses), use GuidingState.
func (m *Mount) IsPulseGuiding() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return time.Now().Before(m.pulseUntil)
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
