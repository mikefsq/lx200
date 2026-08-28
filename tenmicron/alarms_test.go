package tenmicron

import "testing"

func TestAlarmLists(t *testing.T) {
	cases := []struct {
		name  string
		cmd   string
		reply string
		call  func(*Mount) ([]Alarm, error)
		want  []Alarm
	}{
		{"active none", ":alarmlistact#", "#", (*Mount).ActiveAlarms, nil},
		{"active one", ":alarmlistact#", "1001#", (*Mount).ActiveAlarms, []Alarm{AlarmElectronicsHighTemperature}},
		{"unack two", ":alarmlistunack#", "1001,1002#", (*Mount).UnacknowledgedAlarms,
			[]Alarm{AlarmElectronicsHighTemperature, AlarmElectronicsLowTemperature}},
		{"unack none", ":alarmlistunack#", "#", (*Mount).UnacknowledgedAlarms, nil},
		{"unknown code passes through", ":alarmlistact#", "1003#", (*Mount).ActiveAlarms, []Alarm{1003}},
	}
	for _, c := range cases {
		m, f := newMount(map[string]string{c.cmd: c.reply})
		got, err := c.call(m)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if f.LastWrite() != c.cmd {
			t.Errorf("%s: wrote %q, want %q", c.name, f.LastWrite(), c.cmd)
		}
		if len(got) != len(c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: got %v, want %v", c.name, got, c.want)
				break
			}
		}
	}
}

func TestAlarmListBadReply(t *testing.T) {
	m, _ := newMount(map[string]string{":alarmlistact#": "1001,oops#"})
	if _, err := m.ActiveAlarms(); err == nil {
		t.Error("non-numeric alarm identifier accepted")
	}
}

func TestAcknowledgeAlarm(t *testing.T) {
	// The identifier is zero-padded to four digits.
	m, f := newMount(map[string]string{":alarmlack1001#": "V#"})
	if err := m.AcknowledgeAlarm(AlarmElectronicsHighTemperature); err != nil {
		t.Errorf("AcknowledgeAlarm: %v", err)
	}
	if f.LastWrite() != ":alarmlack1001#" {
		t.Errorf("wrote %q", f.LastWrite())
	}

	m2, f2 := newMount(map[string]string{":alarmlack0007#": "E#"})
	if err := m2.AcknowledgeAlarm(7); err == nil {
		t.Error(`"E#" (invalid identifier) reported as success`)
	}
	if f2.LastWrite() != ":alarmlack0007#" {
		t.Errorf("wrote %q, want zero-padded", f2.LastWrite())
	}

	m3, f3 := newMount(nil)
	if err := m3.AcknowledgeAlarm(10000); err == nil {
		t.Error("out-of-range identifier accepted")
	}
	if f3.LastWrite() != "" {
		t.Errorf("out-of-range identifier still hit the wire: %q", f3.LastWrite())
	}
}

func TestAlarmString(t *testing.T) {
	if s := AlarmElectronicsLowTemperature.String(); s != "electronics low temperature" {
		t.Errorf("String() = %q", s)
	}
	if s := Alarm(1234).String(); s != "alarm 1234" {
		t.Errorf("unknown alarm String() = %q", s)
	}
}
