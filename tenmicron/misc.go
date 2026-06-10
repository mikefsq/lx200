package tenmicron

import (
	"fmt"
	"strconv"
	"strings"
)

// ParallacticAngle returns the parallactic angle in degrees, accounting for the
// mount's actual pole alignment — for driving a field derotator (:GPA#).
func (m *Mount) ParallacticAngle() (float64, error) { return m.getFloat(":GPA#") }

// ParallacticSpeed returns the parallactic speed wrt the mount orientation (:GPAS#).
func (m *Mount) ParallacticSpeed() (float64, error) { return m.getFloat(":GPAS#") }

// ParallacticAngleZenith returns the parallactic angle with respect to zenith
// (:GPAZ#).
func (m *Mount) ParallacticAngleZenith() (float64, error) { return m.getFloat(":GPAZ#") }

// FinalApproachTimeConstant returns the final-approach time constant in seconds
// (:GFAtc#), or an error if the function is unsupported (reply "E#").
func (m *Mount) FinalApproachTimeConstant() (float64, error) {
	s, err := m.Get(":GFAtc#")
	if err != nil {
		return 0, err
	}
	s = strings.TrimSpace(s)
	if s == "E" {
		return 0, fmt.Errorf("gotenmicron: final-approach mode not supported")
	}
	return strconv.ParseFloat(s, 64)
}

// StopCommLog ends the communication log (:stoplog#).
func (m *Mount) StopCommLog() error { return m.Blind(":stoplog#") }

// CommLog returns the communication-log text, up to ~256 KiB (:getlog#). Note: the
// reply is read up to the first '#', so a log containing a '#' may be truncated.
func (m *Mount) CommLog() (string, error) { return m.Get(":getlog#") }

// EventLog returns the event-log text, up to ~3 KiB (:evlog#). Same '#' caveat as
// CommLog.
func (m *Mount) EventLog() (string, error) { return m.Get(":evlog#") }
