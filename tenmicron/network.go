package tenmicron

import (
	"fmt"
	"strings"
)

// Network configuration. The setters reconfigure the mount's LAN/wireless interface and
// wake-on-LAN; changing the parameters of the connection you are currently using may
// drop it. SSID/key are escaped on the wire (see escape.go).

// EthernetMAC returns the MAC address of the wired Ethernet interface (:GMAC#,
// "NN:NN:NN:NN:NN:NN"), or "" if the mount has no Ethernet interface. (Firmware ≥ 2.14.11.)
func (m *Mount) EthernetMAC() (string, error) { return m.macAddr(":GMAC#") }

// WirelessMAC returns the MAC address of the wireless interface (:GMACW#), or "" if the
// mount has no wireless interface. (Firmware ≥ 2.14.11.)
func (m *Mount) WirelessMAC() (string, error) { return m.macAddr(":GMACW#") }

func (m *Mount) macAddr(cmd string) (string, error) {
	s, err := m.Get(cmd)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(s), nil // "" (a bare "#") when the interface is absent
}

// SetLANDHCP configures the wired LAN interface to obtain its address by DHCP (:SIP1#).
// (Firmware ≥ 2.10.)
func (m *Mount) SetLANDHCP() error { return m.netResult(":SIP1#") }

// SetLANStatic configures the wired LAN interface with a fixed address
// (:SIP0,ip,mask,gateway#). (Firmware ≥ 2.10.)
func (m *Mount) SetLANStatic(ip, netmask, gateway string) error {
	return m.netResult(fmt.Sprintf(":SIP0,%s,%s,%s#", ip, netmask, gateway))
}

// SetWakeOnLAN enables/disables wake-on-LAN (:SWOLN#). Available only on Q-TYPE2012 /
// Q-TYPE2016 electronics boxes (firmware ≥ 2.15.7); returns an error otherwise.
func (m *Mount) SetWakeOnLAN(on bool) error {
	return must(m.Ack(fmt.Sprintf(":SWOL%d#", b2i(on))))
}

// ShutdownWireless shuts down the wireless interface (:SWRLC#). Wireless-equipped mounts
// only (firmware ≥ 2.12.3).
func (m *Mount) ShutdownWireless() error { return m.netResult(":SWRLC#") }

// WakeOnLANState reads the wake-on-LAN configuration (:GWOL#, Q-TYPE2012/2016 control
// boxes, firmware ≥ 2.15.7): unavailable, disabled, or enabled.
func (m *Mount) WakeOnLANState() (FeatureState, error) { return m.featureState(":GWOL#") }

// StartWirelessScan starts scanning for wireless access points (:GWRSC#) and reports
// whether a wireless adapter is available; read the results with WirelessAccessPoints.
// (Firmware ≥ 2.10.) On a mount configured as a hotspot this interrupts the connection.
func (m *Mount) StartWirelessScan() (available bool, err error) { return m.getBool(":GWRSC#") }

// WirelessAccessPoints returns the access points the last scan found (:GWRAP#), or an
// empty slice when none are found or no wireless adapter is present. Names are
// unescaped. (Firmware ≥ 2.9.8.)
func (m *Mount) WirelessAccessPoints() ([]string, error) {
	s, err := m.Get(":GWRAP#")
	if err != nil {
		return nil, err
	}
	if s = strings.TrimSpace(s); s == "" || s == "0" { // "0" = no wireless available
		return nil, nil
	}
	var aps []string
	for _, name := range strings.Split(strings.TrimPrefix(s, "1"), ",") { // leading '1' flag
		if name != "" {
			aps = append(aps, unescapeString(name))
		}
	}
	return aps, nil
}

// WirelessEncryption is the wireless security mode for the ConfigureWireless* methods.
type WirelessEncryption string

const (
	WirelessWEP WirelessEncryption = "WEP" // WEP
	WirelessWPA WirelessEncryption = "WPA" // WPA-PSK
)

// ConfigureWirelessHotspot sets the wireless interface to host a hotspot other devices
// join (:SWRL1,…#): the mount serves ssid with the given encryption/key at ip/netmask.
// Wireless-equipped mounts only (firmware ≥ 2.9.8; WPA hotspots need ≥ 2.10.5).
func (m *Mount) ConfigureWirelessHotspot(ssid string, enc WirelessEncryption, key, ip, netmask string) error {
	return m.netResult(fmt.Sprintf(":SWRL1,%s,%s,%s,%s,%s#",
		escapeString(ssid), enc, escapeString(key), ip, netmask))
}

// ConfigureWirelessClientDHCP joins an existing network as a client, taking its address
// from that network's DHCP server (:SWRL0,…,DHCP#).
func (m *Mount) ConfigureWirelessClientDHCP(ssid string, enc WirelessEncryption, key string) error {
	return m.netResult(fmt.Sprintf(":SWRL0,%s,%s,%s,DHCP#",
		escapeString(ssid), enc, escapeString(key)))
}

// ConfigureWirelessClientStatic joins an existing network as a client with a fixed
// address (:SWRL0,…,ip,netmask,gateway#).
func (m *Mount) ConfigureWirelessClientStatic(ssid string, enc WirelessEncryption, key, ip, netmask, gateway string) error {
	return m.netResult(fmt.Sprintf(":SWRL0,%s,%s,%s,%s,%s,%s#",
		escapeString(ssid), enc, escapeString(key), ip, netmask, gateway))
}

// DiscoveryService is the state of the mount's network discovery service (:NTGdisc#).
type DiscoveryService struct {
	Available bool
	Active    bool
	Name      string
}

// DiscoveryService reads the network discovery-service configuration (:NTGdisc#):
// whether it is available on this mount, whether it is active, and its advertised name.
func (m *Mount) DiscoveryService() (DiscoveryService, error) {
	s, err := m.Get(":NTGdisc#")
	if err != nil {
		return DiscoveryService{}, err
	}
	f := strings.SplitN(strings.TrimSpace(s), ",", 2)
	var ds DiscoveryService
	switch f[0] {
	case "1": // "1,name#" available and active
		ds.Available, ds.Active = true, true
	case "0": // "0#" not available; "0,name#" available but not active
		ds.Available = len(f) == 2
	}
	if len(f) == 2 {
		ds.Name = f[1]
	}
	return ds, nil
}

// ConfigureDiscoveryService enables/disables the network discovery service, optionally
// setting its advertised name (:NTSdiscN[,name]#). An empty name omits the name field;
// a name must not contain ',' or '#'.
func (m *Mount) ConfigureDiscoveryService(active bool, name string) error {
	cmd := fmt.Sprintf(":NTSdisc%d", b2i(active))
	if name != "" {
		cmd += "," + name
	}
	return m.netResult(cmd + "#")
}

// WebInterfaceActive reports whether the mount's web interface is active (:NTGweb#).
func (m *Mount) WebInterfaceActive() (bool, error) { return m.getBool(":NTGweb#") }

// SetWebInterface enables/disables the mount's web interface (:NTSwebN#).
func (m *Mount) SetWebInterface(active bool) error {
	return m.netResult(fmt.Sprintf(":NTSweb%d#", b2i(active)))
}

// netResult interprets a network-config reply read to '#': "1#" success, "0#" failure.
func (m *Mount) netResult(cmd string) error {
	s, err := m.Get(cmd)
	if err != nil {
		return err
	}
	if strings.TrimSpace(s) != "1" {
		return fmt.Errorf("gotenmicron: network config rejected (%q)", s)
	}
	return nil
}
