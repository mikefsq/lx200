package tenmicron

import (
	"fmt"
	"strconv"
	"strings"
)

// Alarm conditions on direct-drive (DDS) mounts (firmware ≥ 3.4). An alarm condition
// does not necessarily indicate a malfunction and does not by itself prevent normal
// operation, but it should be addressed by the user.
//
// A raised alarm is both *active* (the condition holds now) and *unacknowledged*. The
// two lists are independent: clearing the condition drops the alarm from ActiveAlarms
// but it stays in UnacknowledgedAlarms until AcknowledgeAlarm (or the keypad) clears
// it — so an alarm can be unacknowledged without being active.
type Alarm int

// The alarm conditions the spec defines. The mount may report identifiers not listed
// here; they are returned as-is.
const (
	AlarmElectronicsHighTemperature Alarm = 1001
	AlarmElectronicsLowTemperature  Alarm = 1002
)

// String describes the alarm, falling back to the bare identifier for codes newer than
// this driver.
func (a Alarm) String() string {
	switch a {
	case AlarmElectronicsHighTemperature:
		return "electronics high temperature"
	case AlarmElectronicsLowTemperature:
		return "electronics low temperature"
	}
	return "alarm " + strconv.Itoa(int(a))
}

// ActiveAlarms lists the alarms whose condition currently holds (:alarmlistact#, DDS
// mounts, firmware ≥ 3.4). It returns nil when none is active.
func (m *Mount) ActiveAlarms() ([]Alarm, error) { return m.alarmList(":alarmlistact#") }

// UnacknowledgedAlarms lists the alarms that have not been acknowledged
// (:alarmlistunack#, DDS mounts, firmware ≥ 3.4), including ones whose condition has
// since cleared. It returns nil when none is outstanding.
func (m *Mount) UnacknowledgedAlarms() ([]Alarm, error) { return m.alarmList(":alarmlistunack#") }

// alarmList reads a comma-separated alarm-identifier list; the mount replies with a
// bare "#" when the list is empty.
func (m *Mount) alarmList(cmd string) ([]Alarm, error) {
	s, err := m.Get(cmd)
	if err != nil {
		return nil, err
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var out []Alarm
	for _, f := range strings.Split(s, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil {
			return nil, fmt.Errorf("gotenmicron: bad %s reply %q: %w", cmd, s, err)
		}
		out = append(out, Alarm(n))
	}
	return out, nil
}

// AcknowledgeAlarm acknowledges alarm a (:alarmlackNNNN#, DDS mounts, firmware ≥ 3.4),
// removing it from UnacknowledgedAlarms; it does not clear the underlying condition, so
// an alarm that is still active stays in ActiveAlarms. Errors if the mount rejects the
// identifier ("E#").
//
// (The spec's prose section spells this command ":alarmlistack…#"; the command
// reference — followed here — spells it ":alarmlack…#".)
func (m *Mount) AcknowledgeAlarm(a Alarm) error {
	if a < 0 || a > 9999 {
		return fmt.Errorf("gotenmicron: alarm identifier %d outside [0, 9999]", int(a))
	}
	s, err := m.Get(fmt.Sprintf(":alarmlack%04d#", int(a)))
	if err != nil {
		return err
	}
	if strings.TrimSpace(s) != "V" { // "V#" acknowledged, "E#" invalid identifier
		return fmt.Errorf("gotenmicron: alarm %d not acknowledged (%q)", int(a), s)
	}
	return nil
}
