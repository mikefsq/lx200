// Package serial provides an lx200.Transport over a serial port — the common
// link for LX200 mounts (Rainbow/RST, ZWO AM5 over USB, OnStep). A USB-serial
// adapter and a native RS-232 port are the same to the OS (a device path at a
// baud rate), so this covers both.
//
// It lives in a subpackage so the core lx200 package stays dependency-free; a
// TCP-only build (e.g. 10Micron) never pulls in the serial library.
package serial

import (
	"fmt"
	"time"

	lx200 "github.com/mikefsq/lx200"
	bugst "go.bug.st/serial"
	"go.bug.st/serial/enumerator"
)

// ReadTimeout is the per-read timeout configured on the port. It must be set
// (and reasonably small) so the core's command framing/deadline can make
// progress: with it, Read returns promptly when bytes arrive and returns
// (0, nil) after the timeout when idle, which lx200.Conn's read loop handles.
const ReadTimeout = 100 * time.Millisecond

// Open opens a serial port at the given baud (8 data bits, 1 stop bit, no
// parity) configured for use as an lx200.Transport. portName is the OS device
// path, e.g. "/dev/cu.usbserial-A10KLC4K", "/dev/ttyUSB0", or "/dev/ttyS0".
func Open(portName string, baud int) (lx200.Transport, error) {
	port, err := bugst.Open(portName, &bugst.Mode{
		BaudRate: baud,
		DataBits: 8,
		Parity:   bugst.NoParity,
		StopBits: bugst.OneStopBit,
	})
	if err != nil {
		return nil, fmt.Errorf("lx200/serial: open %s @ %d: %w", portName, baud, err)
	}
	if err := port.SetReadTimeout(ReadTimeout); err != nil {
		port.Close()
		return nil, fmt.Errorf("lx200/serial: set read timeout on %s: %w", portName, err)
	}
	return port, nil
}

// PortInfo describes a discovered serial port (a thin view over the enumerator)
// so per-mount libraries can auto-select their device by USB VID/PID/serial.
type PortInfo struct {
	Name         string // device path
	IsUSB        bool
	VID, PID     string // USB vendor/product IDs (hex, e.g. "0403"/"6001")
	SerialNumber string
}

// List enumerates the serial ports with USB details, for auto-detection. The
// per-mount library applies its own VID/PID/serial match (that logic is
// device-specific, so it is not in this helper).
func List() ([]PortInfo, error) {
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
