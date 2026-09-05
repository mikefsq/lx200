//go:build !darwin

package serial

import (
	"fmt"

	"go.bug.st/serial/enumerator"
)

// listPorts enumerates serial ports with USB identifiers.
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
