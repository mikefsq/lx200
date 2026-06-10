//go:build darwin

package serial

import (
	"fmt"
	"strings"

	bugst "go.bug.st/serial"
)

// listPorts enumerates serial ports on macOS without the enumerator's cgo (IOKit)
// path — which has no CGO_ENABLED=0 build and would break cross-compilation to
// darwin. go.bug.st/serial's GetPortsList is pure Go and returns /dev/cu.* and
// /dev/tty.* device names; USB VID/PID/serial are not available cgo-free, so those
// fields are left empty (IsUSB is inferred from the macOS USB-VCP naming). A
// per-mount Find falls back to the name convention when VID is empty (see rst.Find).
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
