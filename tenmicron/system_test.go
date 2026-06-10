package tenmicron

import (
	"testing"
	"time"
)

func TestPrecisionSetters(t *testing.T) {
	cases := []struct {
		call func(*Mount) error
		want string
	}{
		{(*Mount).SetPrecisionLow, ":U0#"},
		{(*Mount).SetPrecisionHigh, ":U1#"},
		{(*Mount).SetPrecisionUltra, ":U2#"},
		{(*Mount).TogglePrecision, ":U#"},
	}
	for _, c := range cases {
		m, f := newMount(nil)
		if err := c.call(m); err != nil || f.LastWrite() != c.want {
			t.Errorf("wrote %q, want %q (%v)", f.LastWrite(), c.want, err)
		}
	}
}

func TestFirmwareInfo(t *testing.T) {
	m, _ := newMount(map[string]string{
		":GVD#": "Jun 02 2026#",
		":GVT#": "12:30:45#",
		":GVZ#": "Q-TYPE#",
		":V#":   "G#",
	})
	if d, err := m.FirmwareDate(); err != nil || !d.Equal(time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("FirmwareDate = %v, %v", d, err)
	}
	if s, err := m.FirmwareTime(); err != nil || s != "12:30:45" {
		t.Errorf("FirmwareTime = %q, %v", s, err)
	}
	if s, err := m.ControlBoxVersion(); err != nil || s != "Q-TYPE" {
		t.Errorf("ControlBoxVersion = %q, %v", s, err)
	}
	if s, err := m.EmulatedFirmwareRev(); err != nil || s != "G" {
		t.Errorf("EmulatedFirmwareRev = %q, %v", s, err)
	}
}

func TestNetworkInfo(t *testing.T) {
	m, _ := newMount(map[string]string{
		":GINQ#": "0#",
		":GIP#":  "192.168.1.50,255.255.255.0,192.168.1.1,D#",
		":GWAV#": "1#",
		":GWID#": "MyAP#",
		":GWUP#": "0#",
	})
	if v, err := m.ConnectionType(); err != nil || v != 0 {
		t.Errorf("ConnectionType = %v, %v; want 0", v, err)
	}
	nc, err := m.WiredNetwork()
	if err != nil || nc.IP != "192.168.1.50" || nc.Netmask != "255.255.255.0" ||
		nc.Gateway != "192.168.1.1" || nc.Flag != "D" {
		t.Errorf("WiredNetwork = %+v, %v", nc, err)
	}
	if ok, err := m.WirelessAvailable(); err != nil || !ok {
		t.Errorf("WirelessAvailable = %v, %v; want true", ok, err)
	}
	if s, err := m.WirelessESSID(); err != nil || s != "MyAP" {
		t.Errorf("WirelessESSID = %q, %v", s, err)
	}
	if s, err := m.WirelessStatus(); err != nil || s != "0" {
		t.Errorf("WirelessStatus = %q, %v", s, err)
	}
}
