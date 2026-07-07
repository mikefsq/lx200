package tenmicron

import (
	"fmt"
	"strings"
)

// RotatorMaxIndex returns the highest usable rotator index (:FocQmax#, shared with
// focusers).
func (m *Mount) RotatorMaxIndex() (int, error) { return m.getInt(":FocQmax#") }

// Rotator controls one of the mount's attached rotators, indexed 1..RotatorMaxIndex.
type Rotator struct {
	m *Mount
	n int
}

// Rotator returns a controller for rotator index n.
func (m *Mount) Rotator(n int) *Rotator { return &Rotator{m: m, n: n} }

// RotatorFrame selects the angle coordinate system for a rotator command.
type RotatorFrame int

const (
	RotatorMechanical RotatorFrame = iota // M: fixed mechanical reference
	RotatorEquatorial                     // R: 0° = north (equatorial)
	RotatorOptical                        // O: optical reference
)

func rotPosVerb(f RotatorFrame) string { return []string{"M", "R", "O"}[f] }
func rotDstVerb(f RotatorFrame) string { return []string{"m", "r", "o"}[f] }

// Available reports whether the rotator is present (:RotQN#).
func (r *Rotator) Available() (bool, error) {
	return r.m.getBool(fmt.Sprintf(":RotQ%d#", r.n))
}

// Info returns the rotator identity name,type,serial (:RotIN#).
func (r *Rotator) Info() (FocuserInfo, error) {
	s, err := r.m.Get(fmt.Sprintf(":RotI%d#", r.n))
	if err != nil {
		return FocuserInfo{}, err
	}
	if strings.TrimSpace(s) == "" {
		return FocuserInfo{}, fmt.Errorf("gotenmicron: rotator %d info unavailable", r.n)
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

// Angle returns the current rotator angle in degrees, in the chosen frame
// (:RotGM/:RotGR/:RotGON#).
func (r *Rotator) Angle(frame RotatorFrame) (float64, error) {
	return r.m.getFloat(fmt.Sprintf(":RotG%s%d#", rotPosVerb(frame), r.n))
}

// Destination returns the target angle in degrees, in the chosen frame
// (:RotGm/:RotGr/:RotGoN#).
func (r *Rotator) Destination(frame RotatorFrame) (float64, error) {
	return r.m.getFloat(fmt.Sprintf(":RotG%s%d#", rotDstVerb(frame), r.n))
}

// SetDestination sets the target angle in degrees, in the chosen frame, and slews
// to it (:RotSm/:RotSr/:RotSoN,+DDD.DDDD#); reports whether accepted.
func (r *Rotator) SetDestination(frame RotatorFrame, deg float64) (bool, error) {
	return r.m.Ack(fmt.Sprintf(":RotS%s%d,%+09.4f#", rotDstVerb(frame), r.n, deg))
}

// Moving reports whether the rotator is slewing toward its destination (:RotDN#).
func (r *Rotator) Moving() (bool, error) { return r.m.getBool(fmt.Sprintf(":RotD%d#", r.n)) }

// Status returns the rotator status code (:RotdN#).
func (r *Rotator) Status() (int, error) { return r.m.getInt(fmt.Sprintf(":Rotd%d#", r.n)) }

// MaxSpeed returns the configured maximum speed in deg/s (:RotGmsN#).
func (r *Rotator) MaxSpeed() (int, error) { return r.m.getInt(fmt.Sprintf(":RotGms%d#", r.n)) }

// SetMaxSpeed sets the maximum speed in deg/s (:RotSmsN,DDD#).
func (r *Rotator) SetMaxSpeed(degPerSec int) (bool, error) {
	return r.m.Ack(fmt.Sprintf(":RotSms%d,%03d#", r.n, degPerSec))
}

// MaxSpeedRange returns the configurable speed range in deg/s (:RotGmsrN#).
func (r *Rotator) MaxSpeedRange() (min, max int, err error) {
	return r.m.getIntPair(fmt.Sprintf(":RotGmsr%d#", r.n))
}

// PositionValid reports whether the rotator position is valid (homed/recovered)
// (:RotGvN#).
func (r *Rotator) PositionValid() (bool, error) {
	return r.m.getBool(fmt.Sprintf(":RotGv%d#", r.n))
}

// Offset returns the keypad display position-angle offset in degrees (:RotGofN#).
func (r *Rotator) Offset() (float64, error) {
	return r.m.getFloat(fmt.Sprintf(":RotGof%d#", r.n))
}

// SetOffset sets the keypad display position-angle offset in degrees
// (:RotSofN,+ZZZ.ZZZZ#).
func (r *Rotator) SetOffset(deg float64) (bool, error) {
	return r.m.Ack(fmt.Sprintf(":RotSof%d,%+09.4f#", r.n, deg))
}

// ZeroMechanical sets the keypad offset so the mechanical position angle reads zero and
// sets the destination to the current angle (:RotSmZN#).
func (r *Rotator) ZeroMechanical() (bool, error) {
	return r.m.getVOK(fmt.Sprintf(":RotSmZ%d#", r.n))
}

// ZeroEquatorial sets the keypad offset so the equatorial position angle reads zero and
// sets the destination to the current angle (:RotSeZN#).
func (r *Rotator) ZeroEquatorial() (bool, error) {
	return r.m.getVOK(fmt.Sprintf(":RotSeZ%d#", r.n))
}

// StartHoming starts the rotator homing procedure (:RotHSN#).
func (r *Rotator) StartHoming() (bool, error) { return r.m.Ack(fmt.Sprintf(":RotHS%d#", r.n)) }

// HomingStatus returns the rotator homing status (:RotHGN#), sharing the focuser
// homing codes (0 idle/failed, 1 in progress, 2 completed).
func (r *Rotator) HomingStatus() (FocuserHoming, error) {
	n, err := r.m.getInt(fmt.Sprintf(":RotHG%d#", r.n))
	return FocuserHoming(n), err
}

// Stop halts the rotator (:RotSqN#, no reply).
func (r *Rotator) Stop() error { return r.m.Blind(fmt.Sprintf(":RotSq%d#", r.n)) }

// getVOK reads a "V#"(ok)/"E#"(error) reply as a boolean.
func (m *Mount) getVOK(cmd string) (bool, error) {
	s, err := m.Get(cmd)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(s) == "V", nil
}
