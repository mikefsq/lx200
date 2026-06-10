//go:build !darwin

package serial

import (
	"fmt"

	"go.bug.st/serial/enumerator"
)

// listPorts enumerates serial ports with USB details via go.bug.st/serial's
// enumerator, which is pure Go on every non-darwin OS and reports VID/PID/serial.
func listPorts() ([]PortInfo, error) {
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return nil, fmt.Errorf("lx200/serial: enumerate ports: %w", err)
	}
	out := make([]PortInfo, 0, len(ports))
	for _, p := range ports {
		out = append(out, PortInfo{
			Name:         p.Name,
			IsUSB:        p.IsUSB,
			VID:          p.VID,
			PID:          p.PID,
			SerialNumber: p.SerialNumber,
		})
	}
	return out, nil
}
