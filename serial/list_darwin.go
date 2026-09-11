//go:build darwin

package serial

import (
	"fmt"
	"strings"

	bugst "go.bug.st/serial"
)

// listPorts lists macOS device names without cgo; USB identifiers are unavailable.
func listPorts() ([]PortInfo, error) {
	names, err := bugst.GetPortsList()
	if err != nil {
		return nil, fmt.Errorf("lx200/serial: list ports: %w", err)
	}
	out := make([]PortInfo, 0, len(names))
	for _, n := range names {
		out = append(out, PortInfo{
			Name:         n,
			IsUSB:        strings.Contains(n, "usbserial") || strings.Contains(n, "usbmodem"),
			SerialNumber: serialFromName(n),
		})
	}
	return out, nil
}

// serialFromName recovers the USB serial from a macOS device node.
//
// The node IS the serial: the driver names it "/dev/cu.usbserial-A10KLC4K" (and
// "/dev/cu.usbmodem<serial>" for CDC devices), so the value the enumerator would need IOKit and
// cgo to read is already in the path. Without this the whole platform reported no serial, and a
// mount could only be bound by a port name that renumbers when devices are plugged in a different
// order.
func serialFromName(name string) string {
	for _, prefix := range []string{"usbserial-", "usbmodem"} {
		if i := strings.Index(name, prefix); i >= 0 {
			return name[i+len(prefix):]
		}
	}
	return ""
}
