// Package serial provides an lx200.Transport over a serial port.
package serial

import (
	"fmt"
	"time"

	lx200 "github.com/mikefsq/lx200"
	bugst "go.bug.st/serial"
)

// ReadTimeout bounds each serial read so command deadlines can be checked.
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

// PortInfo describes a serial port. On macOS, USB identifiers are empty and
// IsUSB is inferred from the device name.
type PortInfo struct {
	Name         string // device path
	IsUSB        bool
	VID, PID     string // USB vendor/product IDs (hex, e.g. "0403"/"6001"); empty on macOS
	SerialNumber string // empty on macOS
}

// List enumerates serial ports with USB details where available.
func List() ([]PortInfo, error) { return listPorts() }
