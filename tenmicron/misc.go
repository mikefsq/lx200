package tenmicron

// ParallacticAngle returns the parallactic angle in degrees, accounting for the
// mount's actual pole alignment — for driving a field derotator (:GPA#).
func (m *Mount) ParallacticAngle() (float64, error) { return m.getFloat(":GPA#") }

// ParallacticSpeed returns the parallactic speed wrt the mount orientation (:GPAS#).
func (m *Mount) ParallacticSpeed() (float64, error) { return m.getFloat(":GPAS#") }

// ParallacticAngleZenith returns the parallactic angle with respect to zenith, degrees
// (:GPAZ#).
func (m *Mount) ParallacticAngleZenith() (float64, error) { return m.getFloat(":GPAZ#") }

// ParallacticSpeedZenith returns the parallactic speed with respect to zenith — the
// rate of change of the zenith parallactic angle — in arcseconds per second (:GPASZ#).
// (Firmware ≥ 2.15.19.)
func (m *Mount) ParallacticSpeedZenith() (float64, error) { return m.getFloat(":GPASZ#") }

// StartCommLog starts logging the commands the mount receives (:startlog#); read it
// with CommLog, end it with StopCommLog.
func (m *Mount) StartCommLog() error { return m.Blind(":startlog#") }

// StopCommLog ends the communication log (:stoplog#).
func (m *Mount) StopCommLog() error { return m.Blind(":stoplog#") }

// CommLog returns the communication-log text, up to ~256 KiB (:getlog#). Note: the
// reply is read up to the first '#', so a log containing a '#' may be truncated.
func (m *Mount) CommLog() (string, error) { return m.Get(":getlog#") }

// EventLog returns the event-log text, up to ~3 KiB (:evlog#). Same '#' caveat as
// CommLog.
func (m *Mount) EventLog() (string, error) { return m.Get(":evlog#") }
