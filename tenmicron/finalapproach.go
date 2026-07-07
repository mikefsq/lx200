package tenmicron

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Final-approach configuration (GM3000HPS / GM4000HPS only, firmware ≥ 2.15; the
// time-constant read from 2.14.21). The "final approach" is the slow, precise last
// phase of a goto. When the final-approach mode is user-defined (SetFinalApproachMode
// true → :SFAmd1#), the mount uses the user time constant and distance limit below;
// the standard mode (false → :SFAmd0#) uses its built-in configuration.

// ErrNotSupported is returned when the mount does not support a function (reply "E#"),
// e.g. the final-approach commands on any mount other than a GM3000HPS/GM4000HPS.
var ErrNotSupported = errors.New("gotenmicron: function not supported on this mount")

// FinalApproachTimeConstant returns the final-approach time constant in seconds, used
// when the final-approach mode is user-defined (:GFAtc#). ErrNotSupported off a
// GM3000/4000HPS.
func (m *Mount) FinalApproachTimeConstant() (float64, error) { return m.faFloat(":GFAtc#") }

// FinalApproachDistanceLimit returns the user-defined final-approach distance limit in
// arcminutes, used when the mode is user-defined (:GFAlm#).
func (m *Mount) FinalApproachDistanceLimit() (float64, error) { return m.faFloat(":GFAlm#") }

// SetFinalApproachDistanceLimit sets the final-approach distance limit in arcminutes
// (0..9.99), used when the mode is user-defined (:SFAlm…#). 0 forces a final approach
// on every goto.
func (m *Mount) SetFinalApproachDistanceLimit(arcmin float64) error {
	if arcmin < 0 || arcmin > 9.99 {
		return fmt.Errorf("gotenmicron: final-approach distance limit %.2f' outside [0, 9.99]", arcmin)
	}
	return m.faResult(fmt.Sprintf(":SFAlm%.2f#", arcmin))
}

// SetFinalApproachTimeConstant sets the final-approach time constant in seconds
// (:SFAtc…#, 0.25..5.00), used when the mode is custom. GM3000/4000HPS only.
func (m *Mount) SetFinalApproachTimeConstant(seconds float64) error {
	if seconds < 0.25 || seconds > 5.00 {
		return fmt.Errorf("gotenmicron: final-approach time constant %.2fs outside [0.25, 5.00]", seconds)
	}
	return m.faResult(fmt.Sprintf(":SFAtc%.2f#", seconds))
}

// FinalApproachMode is the final-approach parameter set (:GFAmd# / :SFAmd#).
type FinalApproachMode int

const (
	FinalApproachStandard FinalApproachMode = 0 // the mount's built-in time-constant/distance-limit
	FinalApproachCustom   FinalApproachMode = 1 // the user-defined parameters (SetFinalApproach*)
)

// FinalApproachMode returns the active final-approach parameter set (:GFAmd#):
// standard or custom. ErrNotSupported off a GM3000/4000HPS.
func (m *Mount) FinalApproachMode() (FinalApproachMode, error) {
	s, err := m.Get(":GFAmd#")
	if err != nil {
		return 0, err
	}
	switch strings.TrimSpace(s) {
	case "E":
		return 0, ErrNotSupported
	case "1":
		return FinalApproachCustom, nil
	default: // "0"
		return FinalApproachStandard, nil
	}
}

// SetFinalApproachMode selects the final-approach parameter set (:SFAmdN#): custom
// (user-defined time-constant/distance-limit) or standard. Enabling custom can leave
// the mount not fully settled after a slew, depending on the load.
func (m *Mount) SetFinalApproachMode(mode FinalApproachMode) error {
	return m.faResult(fmt.Sprintf(":SFAmd%d#", int(mode)))
}

// faFloat reads a final-approach getter that replies a float or "E#" (not supported).
func (m *Mount) faFloat(cmd string) (float64, error) {
	s, err := m.Get(cmd)
	if err != nil {
		return 0, err
	}
	if s = strings.TrimSpace(s); s == "E" {
		return 0, ErrNotSupported
	}
	return strconv.ParseFloat(s, 64)
}

// faResult interprets a final-approach setter reply: "1#" ok, "0#" out of range /
// failed, "E#" not supported.
func (m *Mount) faResult(cmd string) error {
	s, err := m.Get(cmd)
	if err != nil {
		return err
	}
	switch strings.TrimSpace(s) {
	case "1":
		return nil
	case "E":
		return ErrNotSupported
	default: // "0" = out of range / failed
		return fmt.Errorf("gotenmicron: %s failed (%q)", cmd, s)
	}
}
