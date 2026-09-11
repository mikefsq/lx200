package am5

import (
	"fmt"
	"strings"

	"github.com/mikefsq/lx200/serial"
)

// Finding an AM5 among the machine's serial ports.
//
// # It has its own VID:PID, and that changes everything
//
// The RST rides an FTDI bridge, 0403:6001 — the same chip an SQM, a weather box and half the
// other instruments on a rig use — so the only way to tell a mount from a dew heater there is to
// open the port and ask. The AM series enumerates as ZWO's own 03C3:4001 (INDIGO's mount_asi
// driver matches the same pair), so on a platform that reports VID and PID the identification is
// the descriptor: no port is opened, nothing else on the bus is disturbed, and a rig with three
// mounts gets three entries told apart by their USB serials.
//
// macOS reports neither VID nor PID without linking IOKit through cgo, which this package will
// not do — every consumer of lx200/serial would inherit it, including headless ones that must
// cross-compile. What macOS does give is the device node, whose name carries the USB serial. So
// the platforms split by DISCOVERY only: listing mounts on macOS confirms each candidate with
// :GVP#, while binding to a known serial opens exactly one node on every platform.
const (
	vendorZWO  = "03C3"
	productAM5 = "4001"
)

// Discovered is an AM-series mount that answered, over either transport.
//
// Port and Serial describe a mount on USB; Addr describes one on the network. Exactly one side is
// set, and which one tells a caller how the mount is reached — a serial pins a cable, an address
// pins a host, and neither can be converted into the other.
type Discovered struct {
	Port    string // USB: the device node it answered on
	Serial  string // USB: the serial that pins this mount across replugs
	Addr    string // network: the host:port it answered on
	Product string // :GVP# product name, e.g. "AM5"
	Version string // :GV# firmware version
}

// DefaultTCPPort is the port ZWO's firmware serves, hardcoded by INDI and INDIGO alike.
const DefaultTCPPort = 4030

// Filter restricts a search to one mount.
type Filter struct {
	Serial string // bind only the mount with this USB serial; empty = any candidate
}

// Report is what a search learned, for a caller that wants to remember it.
type Report struct {
	Port    string
	Serial  string
	Product string
	Version string
}

// Discover lists the AM-series mounts attached to this machine, WITHOUT keeping any of them open.
//
// Every candidate is confirmed by asking: the descriptor narrows the field to ZWO devices where
// the platform reports one, and :GVP# proves the thing on the other end is a mount rather than
// another ZWO peripheral on the same VID. Ports another process holds are skipped — a busy port
// cannot be asked, and a mount already open is one this machine has found.
func Discover() ([]Discovered, error) {
	ports, err := serial.List()
	if err != nil {
		return nil, err
	}
	var out []Discovered
	for _, p := range candidates(ports, Filter{}) {
		product, version, ok := probe(p.Name)
		if !ok {
			continue
		}
		out = append(out, Discovered{Port: p.Name, Serial: p.SerialNumber, Product: product, Version: version})
	}
	return out, nil
}

// FindMatching opens the AM-series mount that satisfies f.
//
// A filter with a serial opens ONE port: the candidate list is narrowed by the descriptor before
// anything is touched, which is the whole reason to store a serial rather than a port path. Only
// an empty filter asks around, and even then it asks in descriptor order rather than walking every
// serial device on the machine.
func FindMatching(f Filter) (*Mount, Report, error) {
	var rep Report
	ports, err := serial.List()
	if err != nil {
		return nil, rep, err
	}
	cands := candidates(ports, f)
	if len(cands) == 0 {
		if f.Serial != "" {
			return nil, rep, fmt.Errorf("am5: no USB serial device with serial %q", f.Serial)
		}
		return nil, rep, fmt.Errorf("am5: no ZWO AM-series mount found (USB %s:%s)", vendorZWO, productAM5)
	}
	for _, c := range cands {
		product, version, ok := probe(c.Name)
		if !ok {
			continue
		}
		rep = Report{Port: c.Name, Serial: c.SerialNumber, Product: product, Version: version}
		m, err := Open(c.Name)
		return m, rep, err
	}
	return nil, rep, fmt.Errorf("am5: no mount answered on %d candidate port(s)", len(cands))
}

// candidates returns the ports worth asking, after applying f.
//
// Two rules, and which one applies is the platform's choice rather than ours: a port that reports
// ZWO's VID and PID is a candidate outright, and a USB port that reports no VID at all is a
// candidate because macOS reports none for anything. A serial in the filter is matched in both
// cases — on macOS against the serial recovered from the device node, which is why binding by
// serial costs no opens there either.
func candidates(ports []serial.PortInfo, f Filter) []serial.PortInfo {
	keep := func(p serial.PortInfo) bool {
		if f.Serial == "" {
			return true
		}
		return serialMatches(p.SerialNumber, f.Serial)
	}
	var out []serial.PortInfo
	for _, p := range ports { // the descriptor says it is a ZWO device
		if p.IsUSB && strings.EqualFold(p.VID, vendorZWO) && strings.EqualFold(p.PID, productAM5) && keep(p) {
			out = append(out, p)
		}
	}
	for _, p := range ports { // no descriptor to go on (macOS): every USB serial node is a maybe
		if p.IsUSB && p.VID == "" && keep(p) {
			out = append(out, p)
		}
	}
	return dedupeAliases(out)
}

// serialMatches compares a port's serial with a configured one, tolerating the interface digit
// macOS appends.
//
// The two platforms report the same mount differently. Linux and Windows read iSerialNumber from
// the descriptor and give "123456"; macOS names the node "/dev/cu.usbmodem1234561" — the serial
// with the CDC interface number stuck on the end — and that suffix is all this package can
// recover without linking IOKit. An entry configured on one machine must still bind on the other,
// so a match is equality or one being the other's prefix. The interface number is a single digit
// in practice, and no two mounts on a machine can differ only by it: they would then share a
// descriptor serial, and neither platform could tell them apart anyway.
func serialMatches(have, want string) bool {
	have, want = strings.TrimSpace(have), strings.TrimSpace(want)
	if have == "" || want == "" {
		return false
	}
	if strings.EqualFold(have, want) {
		return true
	}
	if len(have) > len(want) {
		return strings.EqualFold(have[:len(want)], want)
	}
	return strings.EqualFold(want[:len(have)], have)
}

// dedupeAliases drops the /dev/tty.* half of a macOS port pair.
//
// Darwin exposes one USB serial device twice — /dev/cu.* to call out, /dev/tty.* to dial in. They
// are the same mount, and the tty. side blocks on carrier detect, so probing it costs a full
// timeout and would list the mount twice if it ever answered.
func dedupeAliases(ports []serial.PortInfo) []serial.PortInfo {
	callout := map[string]bool{}
	for _, p := range ports {
		if strings.HasPrefix(p.Name, "/dev/cu.") {
			callout[strings.TrimPrefix(p.Name, "/dev/cu.")] = true
		}
	}
	var out []serial.PortInfo
	for _, p := range ports {
		if strings.HasPrefix(p.Name, "/dev/tty.") && callout[strings.TrimPrefix(p.Name, "/dev/tty.")] {
			continue
		}
		out = append(out, p)
	}
	return out
}

// probe asks a port what it is, closing it again.
//
// The product string is the test INDIGO's driver uses and the one worth copying: an AM-series
// mount answers :GVP# with "AM" and a digit ("AM5", "AM3"). Anything else on the port — another
// instrument, a device that ignores the command, silence — fails the same way, so this is safe to
// run against ports that are not ours.
func probe(portName string) (product, version string, ok bool) {
	m, err := Open(portName)
	if err != nil {
		return "", "", false // busy (another driver holds it) or not openable
	}
	defer m.Close()
	p, err := m.Product()
	if err != nil || !isAMProduct(p) {
		return "", "", false
	}
	v, err := m.Firmware()
	if err != nil {
		v = "" // an unversioned mount is still a mount
	}
	return p, v, true
}

// isAMProduct reports whether a :GVP# reply names an AM-series mount.
func isAMProduct(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 3 || !strings.EqualFold(s[:2], "AM") {
		return false
	}
	return s[2] >= '0' && s[2] <= '9'
}
