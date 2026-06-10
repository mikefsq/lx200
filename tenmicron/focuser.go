package tenmicron

import (
	"fmt"
	"strconv"
	"strings"
)

// FocuserMaxIndex returns the highest index usable in focuser/rotator commands
// (:FocQmax#).
func (m *Mount) FocuserMaxIndex() (int, error) { return m.getInt(":FocQmax#") }

// Focuser controls one of the mount's attached focusers, indexed 1..FocuserMaxIndex.
type Focuser struct {
	m *Mount
	n int
}

// Focuser returns a controller for focuser index n.
func (m *Mount) Focuser(n int) *Focuser { return &Focuser{m: m, n: n} }

func (f *Focuser) cmd(verb string) string { return fmt.Sprintf(":Foc%s%d#", verb, f.n) }
func (f *Focuser) cmdp(verb, params string) string {
	return fmt.Sprintf(":Foc%s%d,%s#", verb, f.n, params)
}

// FocuserStatus is the focuser state code (:FocdN#).
type FocuserStatus int

const (
	FocuserStopped     FocuserStatus = 0
	FocuserTracking    FocuserStatus = 1
	FocuserManual      FocuserStatus = 2
	FocuserSlewing     FocuserStatus = 3
	FocuserSlewingHome FocuserStatus = 4
	FocuserStopping    FocuserStatus = 5
	FocuserLost        FocuserStatus = 6  // lost position, needs homing
	FocuserOverload    FocuserStatus = 97 // stopped due to overload
	FocuserUnknown     FocuserStatus = 98
	FocuserError       FocuserStatus = 99 // error or invalid index
)

// FocuserInfo is the focuser identity (:FocIN#).
type FocuserInfo struct{ Name, Type, Serial string }

// Available reports whether the focuser is present (:FocQN#).
func (f *Focuser) Available() (bool, error) { return f.m.getBool(f.cmd("Q")) }

// Info returns the focuser identity (:FocIN#).
func (f *Focuser) Info() (FocuserInfo, error) {
	s, err := f.m.Get(f.cmd("I"))
	if err != nil {
		return FocuserInfo{}, err
	}
	if strings.TrimSpace(s) == "" {
		return FocuserInfo{}, fmt.Errorf("gotenmicron: focuser %d info unavailable", f.n)
	}
	p := strings.Split(s, ",")
	var fi FocuserInfo
	if len(p) > 0 {
		fi.Name = p[0]
	}
	if len(p) > 1 {
		fi.Type = p[1]
	}
	if len(p) > 2 {
		fi.Serial = p[2]
	}
	return fi, nil
}

// Position returns the current focuser position in µm (:FocGuFN#).
func (f *Focuser) Position() (int, error) { return f.m.getInt(f.cmd("GuF")) }

// Destination returns the target position in µm, not temperature-compensated
// (:FocGufN#).
func (f *Focuser) Destination() (int, error) { return f.m.getInt(f.cmd("Guf")) }

// SetDestination sets the target position in µm (:FocSuFN,+PPPPPP#); reports
// whether the value was accepted. Begin the move with StartMove.
func (f *Focuser) SetDestination(microns int) (bool, error) {
	return f.m.Ack(f.cmdp("SuF", fmt.Sprintf("%+07d", microns)))
}

// StartMove starts motion toward the set destination (:FocSSN#).
func (f *Focuser) StartMove() (bool, error) { return f.m.Ack(f.cmd("SS")) }

// Moving reports whether the focuser is moving toward its destination (:FocDN#).
func (f *Focuser) Moving() (bool, error) { return f.m.getBool(f.cmd("D")) }

// Status returns the focuser state code (:FocdN#).
func (f *Focuser) Status() (FocuserStatus, error) {
	n, err := f.m.getInt(f.cmd("d"))
	return FocuserStatus(n), err
}

// Temperature returns the focuser temperature in °C (:FocTN#).
func (f *Focuser) Temperature() (float64, error) { return f.m.getFloat(f.cmd("T")) }

// MaxSpeed returns the configured maximum speed in µm/s (:FocGmsN#).
func (f *Focuser) MaxSpeed() (int, error) { return f.m.getInt(f.cmd("Gms")) }

// SetMaxSpeed sets the maximum speed in µm/s (:FocSmsN,DDDDD#).
func (f *Focuser) SetMaxSpeed(micronsPerSec int) (bool, error) {
	return f.m.Ack(f.cmdp("Sms", fmt.Sprintf("%05d", micronsPerSec)))
}

// MaxSpeedRange returns the configurable speed range in µm/s (:FocGmsrN#).
func (f *Focuser) MaxSpeedRange() (min, max int, err error) {
	return f.m.getIntPair(f.cmd("Gmsr"))
}

// PositionValid reports whether the focuser position is valid (homed/recovered)
// (:FocGvN#).
func (f *Focuser) PositionValid() (bool, error) { return f.m.getBool(f.cmd("Gv")) }

// Range returns the allowed positioning range in µm relative to the home position
// (:FocGZN#).
func (f *Focuser) Range() (min, max int, err error) { return f.m.getIntPair(f.cmd("GZ")) }

// SetRange sets the allowed positioning range in µm relative to home
// (:FocSZN,+MMMMM,+NNNNN#); max must be ≥ min+1000.
func (f *Focuser) SetRange(min, max int) (bool, error) {
	return f.m.Ack(f.cmdp("SZ", fmt.Sprintf("%+06d,%+06d", min, max)))
}

// StartHoming starts the focuser homing procedure (:FocHSN#).
func (f *Focuser) StartHoming() (bool, error) { return f.m.Ack(f.cmd("HS")) }

// FocuserHoming is the homing-operation status (:FocHGN#).
type FocuserHoming int

const (
	FocuserHomingIdleOrFailed FocuserHoming = 0
	FocuserHomingInProgress   FocuserHoming = 1
	FocuserHomingCompleted    FocuserHoming = 2
)

// HomingStatus returns the focuser homing status (:FocHGN#).
func (f *Focuser) HomingStatus() (FocuserHoming, error) {
	n, err := f.m.getInt(f.cmd("HG"))
	return FocuserHoming(n), err
}

// MoveAtSpeed starts continuous motion at the given signed speed in µm/s
// (:FocSsN,+DDDDDD#); reports whether accepted. Stop with Stop.
func (f *Focuser) MoveAtSpeed(micronsPerSec int) (bool, error) {
	return f.m.Ack(f.cmdp("Ss", fmt.Sprintf("%+07d", micronsPerSec)))
}

// Stop halts the focuser (:FocSqN#, no reply).
func (f *Focuser) Stop() error { return f.m.Blind(f.cmd("Sq")) }

// --- Legacy focuser-1 motion commands (:F…#) --------------------------------

// Focuser1In / Focuser1Out start motion of focuser 1 inward/outward (:F+#/:F-#).
func (m *Mount) Focuser1In() error  { return m.Blind(":F+#") }
func (m *Mount) Focuser1Out() error { return m.Blind(":F-#") }

// Focuser1SpeedFast / Focuser1SpeedSlow set focuser-1 speed (:FF#/:FS#).
func (m *Mount) Focuser1SpeedFast() error { return m.Blind(":FF#") }
func (m *Mount) Focuser1SpeedSlow() error { return m.Blind(":FS#") }

// Focuser1Halt halts focuser-1 motion (:FQ#).
func (m *Mount) Focuser1Halt() error { return m.Blind(":FQ#") }

// Focuser1Speed sets focuser-1 speed by index n (:Fn#): 1=0.01mm/s, 2=0.1mm/s, …
func (m *Mount) Focuser1Speed(n int) error { return m.Blind(fmt.Sprintf(":F%d#", n)) }

// --- shared numeric-reply helpers -------------------------------------------

// getIntPair parses an "A,B#" reply of two integers; "E#" is an error.
func (m *Mount) getIntPair(cmd string) (int, int, error) {
	s, err := m.Get(cmd)
	if err != nil {
		return 0, 0, err
	}
	s = strings.TrimSpace(s)
	if s == "E" {
		return 0, 0, fmt.Errorf("gotenmicron: %s error", cmd)
	}
	p := strings.SplitN(s, ",", 2)
	if len(p) != 2 {
		return 0, 0, fmt.Errorf("gotenmicron: bad pair reply %q", s)
	}
	a, _ := strconv.Atoi(strings.TrimSpace(p[0]))
	b, _ := strconv.Atoi(strings.TrimSpace(p[1]))
	return a, b, nil
}
