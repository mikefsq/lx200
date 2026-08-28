package tenmicron

import (
	"fmt"
	"strings"
	"time"
)

// Precision selectors. Connect sets ultra precision (:U2#) and the typed
// coordinate/target getters assume it — changing precision changes the mount's reply
// formats, so use these with care.

// SetPrecisionLow selects low precision (:U0#).
func (m *Mount) SetPrecisionLow() error { return m.Blind(":U0#") }

// SetPrecisionHigh selects high precision (:U1#).
func (m *Mount) SetPrecisionHigh() error { return m.Blind(":U1#") }

// SetPrecisionUltra selects ultra precision (:U2#) — the mode Connect sets.
func (m *Mount) SetPrecisionUltra() error { return m.Blind(":U2#") }

// TogglePrecision cycles the precision mode (:U#).
func (m *Mount) TogglePrecision() error { return m.Blind(":U#") }

// Product returns the mount product name (:GVP#).
func (m *Mount) Product() (string, error) { return m.Get(":GVP#") }

// HardwareID returns a unique 20-digit (64-bit) hardware identifier for the mount
// (:GETID#). It is stable across connections (e.g. serial vs LAN) and changes only if
// the mount is serviced — use it to tell whether two connections reach the same mount.
// (Firmware ≥ 2.9.11.)
func (m *Mount) HardwareID() (string, error) {
	s, err := m.Get(":GETID#")
	return strings.TrimSpace(s), err
}

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

// WiredNetwork reads the wired Ethernet IP configuration (:GIP#).
func (m *Mount) WiredNetwork() (NetworkConfig, error) { return m.netConfig(":GIP#") }

// WirelessNetwork reads the wireless IP configuration (:GIPW#).
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

// Shutdown shuts the mount down (:shutdown#). Electronics model 2012 or later only; do
// NOT remove power until this returns without error. Reports whether it was accepted.
func (m *Mount) Shutdown() error { return must(m.Ack(":shutdown#")) }

// BaudRate is a serial baud-rate selection for SetBaudRate (:SBn#), RS-232 only.
type BaudRate int

const (
	Baud115200 BaudRate = 0 // 115.2 kbaud
	Baud57600  BaudRate = 1 // 57.6 kbaud
	Baud38400  BaudRate = 2 // 38.4 kbaud
	Baud19200  BaudRate = 4 // 19.2 kbaud
	Baud9600   BaudRate = 6 // 9600 baud
	Baud4800   BaudRate = 7 // 4800 baud
	Baud2400   BaudRate = 8 // 2400 baud
	Baud1200   BaudRate = 9 // 1200 baud
)

// SetBaudRate sets the RS-232 baud rate (:SBn#); it affects the serial link only. The
// mount acknowledges at the OLD rate and then switches, so the caller must reconfigure
// its own port to match afterwards. Reports whether the mount accepted the rate.
func (m *Mount) SetBaudRate(r BaudRate) error { return must(m.Ack(fmt.Sprintf(":SB%d#", int(r)))) }

// EmulateLX200 selects LX200 emulation mode (:EMULX#). In the ultra-precision mode
// Connect sets, the emulation modes are equivalent.
func (m *Mount) EmulateLX200() error { return m.Blind(":EMULX#") }

// EmulateAstroPhysics selects Astro-Physics-compatible emulation mode (:EMUAP#). In
// ultra-precision mode the emulation modes are equivalent.
func (m *Mount) EmulateAstroPhysics() error { return m.Blind(":EMUAP#") }

// FeatureState is the availability/enabled tri-state some configuration reads return
// (N# not available/configurable, 0# available-but-off, 1# available-and-on), e.g.
// :GAPO# (auto power-on) and :GWOL# (wake-on-LAN).
type FeatureState int

const (
	FeatureUnavailable FeatureState = iota // "N": not available or not configurable
	FeatureDisabled                        // "0": available but not enabled
	FeatureEnabled                         // "1": available and enabled
)

// featureState reads an N/0/1 tri-state configuration reply.
func (m *Mount) featureState(cmd string) (FeatureState, error) {
	s, err := m.Get(cmd)
	if err != nil {
		return FeatureUnavailable, err
	}
	switch strings.TrimSpace(s) {
	case "1":
		return FeatureEnabled, nil
	case "0":
		return FeatureDisabled, nil
	default: // "N"
		return FeatureUnavailable, nil
	}
}

// AutoPowerOnState reads the auto-power-on configuration (:GAPO#, Q-TYPE 2024 control
// box, firmware ≥ 3.2.5): unavailable, disabled, or enabled.
func (m *Mount) AutoPowerOnState() (FeatureState, error) { return m.featureState(":GAPO#") }

// SetAutoPowerOn enables/disables auto power-on (:SAPOn#, Q-TYPE 2024 control box,
// firmware ≥ 3.2.5). Errors if the mount could not configure it.
func (m *Mount) SetAutoPowerOn(on bool) error {
	return must(m.Ack(fmt.Sprintf(":SAPO%d#", b2i(on))))
}

// --- Mount classification (from :GVP#, read at Connect) ---------------------

// MountClass is the geometry/series of the connected mount, parsed from the :GVP#
// product name.
type MountClass struct {
	Product string // raw :GVP# product name, e.g. "10micron GM1000HPS"
	AltAz   bool   // an AZ-series (altazimuth) mount rather than a GM (German equatorial)
	GM4000  bool   // the GM4000/AZ4000 class, which uses a 0.75 RA slew-rate ratio
	DDS     bool   // a direct-drive mount (AZ2500DDS/AZ5000DDS/AZ6000DDS): alarms, DDS Gstat codes
}

// parseMountClass classifies a :GVP# product name (e.g. "10micron GM4000HPS").
func parseMountClass(product string) MountClass {
	p := strings.TrimSpace(product)
	up := strings.ToUpper(p)
	return MountClass{
		Product: p,
		AltAz:   strings.Contains(up, "AZ"),
		GM4000:  strings.Contains(up, "4000"),
		DDS:     strings.Contains(up, "DDS"),
	}
}

// MountClass returns the connected mount's classification, read from :GVP# at Connect
// (a zero MountClass for a directly-constructed Mount).
func (m *Mount) MountClass() MountClass { return m.mountClass }

// RASlewRatio is the factor the mount applies to the RA/azimuth axis's maximum slew
// rate relative to the declination/altitude axis: 0.75 on the GM4000/AZ4000 class, 1.0
// otherwise. It mirrors the vendor driver's get_ra_ratio.
func (m *Mount) RASlewRatio() float64 {
	if m.mountClass.GM4000 {
		return 0.75
	}
	return 1.0
}

// MountConfig is the decoded :GCFG# configuration (firmware ≥ 3.2.5).
type MountConfig struct {
	AltAz    bool // 'A' altazimuth vs 'E' equatorial
	Fork     bool // 'F' fork vs 'G' german
	Southern bool // 'S' southern vs 'N' northern hemisphere
	Homed    bool // 'H' a home position has been found vs 'h' not
}

// MountConfiguration reads the live mount configuration flags (:GCFG#, firmware ≥
// 3.2.5): geometry (altaz/equatorial), mounting (fork/german), hemisphere, and whether
// a home position has been found. Unlike MountClass (a static product classification)
// this reflects the mount's current configured state.
func (m *Mount) MountConfiguration() (MountConfig, error) {
	s, err := m.Get(":GCFG#")
	if err != nil {
		return MountConfig{}, err
	}
	f := strings.Split(strings.TrimSpace(s), ",")
	if len(f) != 4 {
		return MountConfig{}, fmt.Errorf("gotenmicron: bad :GCFG# reply %q", s)
	}
	return MountConfig{
		AltAz:    f[0] == "A",
		Fork:     f[1] == "F",
		Southern: f[2] == "S",
		Homed:    f[3] == "H",
	}, nil
}
