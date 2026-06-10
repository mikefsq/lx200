// Package serial provides an lx200.Transport over a serial port — the common
// link for LX200 mounts (Rainbow/RST, ZWO AM5 over USB, OnStep). A USB-serial
// adapter and a native RS-232 port are the same to the OS (a device path at a
// baud rate), so this covers both.
//
// It lives in a subpackage so the core lx200 package stays dependency-free; a
// TCP-only build (e.g. 10Micron) never pulls in the serial library.
//
// Only go.bug.st/serial's pure-Go paths are used, so the package builds for any
// target with CGO_ENABLED=0 (the library's lone cgo path is its macOS USB
// enumerator, which List avoids on darwin — see PortInfo / listPorts).
package serial

import (
	"fmt"
	"time"

	lx200 "github.com/mikefsq/lx200"
	bugst "go.bug.st/serial"
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

// PortInfo describes a discovered serial port so per-mount libraries can
// auto-select their device by USB VID/PID/serial.
//
// VID/PID/SerialNumber come from go.bug.st/serial's USB enumerator, which is pure
// Go on every OS EXCEPT macOS, where it requires cgo (IOKit) and has no
// CGO_ENABLED=0 build. To keep the library buildable for any target with cgo off
// (including cross-compiling to darwin), List does not use the enumerator on macOS:
// there the fields VID/PID/SerialNumber are empty and only Name (and a best-effort
// IsUSB) is populated. A per-mount Find should therefore fall back to the device's
// name convention when VID is unavailable (see rst.Find).
type PortInfo struct {
	Name         string // device path
	IsUSB        bool
	VID, PID     string // USB vendor/product IDs (hex, e.g. "0403"/"6001"); empty on macOS
	SerialNumber string // empty on macOS
}

// List enumerates the serial ports for auto-detection (implementation is per-OS:
// the USB enumerator off macOS, name-only on macOS — see PortInfo). The per-mount
// library applies its own VID/PID/name match (that logic is device-specific, so it
// is not in this helper).
func List() ([]PortInfo, error) { return listPorts() }
