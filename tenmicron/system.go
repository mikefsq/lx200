package tenmicron

import (
	"fmt"
	"strings"
	"time"
)

// Precision selectors (:U0#/:U1#/:U2#/:U#). Connect sets ultra precision (:U2#)
// and the typed coordinate/target getters assume it — changing precision changes
// the mount's reply formats, so use these with care.
func (m *Mount) SetPrecisionLow() error   { return m.Blind(":U0#") }
func (m *Mount) SetPrecisionHigh() error  { return m.Blind(":U1#") }
func (m *Mount) SetPrecisionUltra() error { return m.Blind(":U2#") }
func (m *Mount) TogglePrecision() error   { return m.Blind(":U#") }

// Product returns the mount product name (:GVP#).
func (m *Mount) Product() (string, error) { return m.Get(":GVP#") }

// FirmwareDate returns the firmware build date (:GVD#, "mmm dd yyyy").
func (m *Mount) FirmwareDate() (time.Time, error) {
	s, err := m.Get(":GVD#")
	if err != nil {
		return time.Time{}, err
	}
	s = strings.TrimSpace(s)
	for _, layout := range []string{"Jan 02 2006", "Jan _2 2006"} {
		if t, e := time.Parse(layout, s); e == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("gotenmicron: unrecognized :GVD# date %q", s)
}

// FirmwareTime returns the firmware build time (:GVT#, "HH:MM:SS").
func (m *Mount) FirmwareTime() (string, error) { return m.Get(":GVT#") }

// ControlBoxVersion returns the control-box hardware version string (:GVZ#).
func (m *Mount) ControlBoxVersion() (string, error) { return m.Get(":GVZ#") }

// EmulatedFirmwareRev returns the emulated LX200 firmware revision (:V#, "G").
func (m *Mount) EmulatedFirmwareRev() (string, error) { return m.Get(":V#") }

// ConnectionType reports the active connection type (:GINQ#): 0 = serial RS-232,
// 1 = GPS or GPS/RS-232 port.
func (m *Mount) ConnectionType() (int, error) { return m.getInt(":GINQ#") }

// NetworkConfig is the mount's IP configuration (:GIP#/:GIPW#).
type NetworkConfig struct {
	IP, Netmask, Gateway string
	Flag                 string // trailing indicator character (e.g. DHCP/static)
}

// WiredNetwork / WirelessNetwork read the wired/wireless IP configuration
// (:GIP#/:GIPW#).
func (m *Mount) WiredNetwork() (NetworkConfig, error)    { return m.netConfig(":GIP#") }
func (m *Mount) WirelessNetwork() (NetworkConfig, error) { return m.netConfig(":GIPW#") }

func (m *Mount) netConfig(cmd string) (NetworkConfig, error) {
	s, err := m.Get(cmd)
	if err != nil {
		return NetworkConfig{}, err
	}
	p := strings.Split(strings.TrimSpace(s), ",")
	var nc NetworkConfig
	if len(p) > 0 {
		nc.IP = p[0]
	}
	if len(p) > 1 {
		nc.Netmask = p[1]
	}
	if len(p) > 2 {
		nc.Gateway = p[2]
	}
	if len(p) > 3 {
		nc.Flag = p[3]
	}
	return nc, nil
}

// WirelessAvailable reports whether a wireless adapter is present (:GWAV#).
func (m *Mount) WirelessAvailable() (bool, error) { return m.getBool(":GWAV#") }

// WirelessESSID returns the connected access-point name, or "" if not connected
// (:GWID#).
func (m *Mount) WirelessESSID() (string, error) { return m.Get(":GWID#") }

// WirelessStatus returns the raw wireless-network status (:GWUP#): "E" =
// unconfigured, "0" = configured, etc. (per spec).
func (m *Mount) WirelessStatus() (string, error) { return m.Get(":GWUP#") }
