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
			Name:  n,
			IsUSB: strings.Contains(n, "usbserial") || strings.Contains(n, "usbmodem"),
		})
	}
	return out, nil
}
