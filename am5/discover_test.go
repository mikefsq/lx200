package am5

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/mikefsq/lx200/serial"
)

// The AM series has ZWO's own VID:PID, so where a platform reports the descriptor the candidate
// list is exact — no dew heater, no SQM, none of the other instruments an FTDI bridge would drag
// in. That is what lets a scan avoid opening ports it has no business opening.
func TestCandidatesPreferTheDescriptor(t *testing.T) {
	ports := []serial.PortInfo{
		{Name: "/dev/ttyUSB0", IsUSB: true, VID: "0403", PID: "6001", SerialNumber: "A10KLC4K"}, // an FTDI instrument
		{Name: "/dev/ttyACM0", IsUSB: true, VID: "03C3", PID: "4001", SerialNumber: "AM5-0001"},
		{Name: "/dev/ttyACM1", IsUSB: true, VID: "03C3", PID: "4001", SerialNumber: "AM5-0002"},
	}
	got := candidates(ports, Filter{})
	if len(got) != 2 {
		t.Fatalf("got %d candidates, want the 2 ZWO devices: %+v", len(got), got)
	}
	// A serial narrows to one mount, which is what makes connect cheap with several attached.
	if got := candidates(ports, Filter{Serial: "am5-0002"}); len(got) != 1 || got[0].Name != "/dev/ttyACM1" {
		t.Errorf("serial filter = %+v, want only ttyACM1 (matched case-insensitively)", got)
	}
}

// macOS reports no VID for anything, so every USB serial node is a maybe — and the serial
// recovered from the device node still pins one mount without opening a thing.
func TestCandidatesFallBackToTheNodeWhereNoDescriptorExists(t *testing.T) {
	ports := []serial.PortInfo{
		{Name: "/dev/cu.usbmodemAM50001", IsUSB: true, SerialNumber: "AM50001"},
		{Name: "/dev/tty.usbmodemAM50001", IsUSB: true, SerialNumber: "AM50001"},
		{Name: "/dev/cu.usbmodemAM50002", IsUSB: true, SerialNumber: "AM50002"},
		{Name: "/dev/cu.Bluetooth-Incoming-Port", IsUSB: false},
	}
	got := candidates(ports, Filter{})
	if len(got) != 2 {
		t.Fatalf("got %d candidates, want 2 (the tty. twin and the Bluetooth port dropped): %+v", len(got), got)
	}
	one := candidates(ports, Filter{Serial: "AM50001"})
	if len(one) != 1 || one[0].Name != "/dev/cu.usbmodemAM50001" {
		t.Errorf("serial filter = %+v, want only the cu. node", one)
	}
}

// The product string is what proves a mount is on the other end; ZWO's vendor id covers their
// cameras and wheels too.
func TestIsAMProduct(t *testing.T) {
	for _, ok := range []string{"AM5", "AM3", "am5", "AM5 ", "AM5Pro"} {
		if !isAMProduct(ok) {
			t.Errorf("%q rejected", ok)
		}
	}
	for _, bad := range []string{"", "AM", "ASI6200", "AMP", "EFW", "Rainbow"} {
		if isAMProduct(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}

// A sweep enumerates the usable hosts of a subnet, and only of a subnet small enough to sweep
// politely: a /24 is 254 hosts and takes about a second, where a /16 would take minutes and look
// like reconnaissance to anything watching the network.
func TestSubnetHostsSkipsNetworkAndBroadcast(t *testing.T) {
	_, n, err := net.ParseCIDR("192.168.4.0/24")
	if err != nil {
		t.Fatal(err)
	}
	hosts := subnetHosts(n)
	if len(hosts) != 254 {
		t.Fatalf("got %d hosts, want 254", len(hosts))
	}
	if hosts[0] != "192.168.4.1" || hosts[len(hosts)-1] != "192.168.4.254" {
		t.Errorf("range = %s … %s", hosts[0], hosts[len(hosts)-1])
	}
	// The mount's own access-point address must be in range: it is the one address worth
	// guessing, and the case an operator hits first.
	found := false
	for _, h := range hosts {
		if h == "192.168.4.1" {
			found = true
		}
	}
	if !found {
		t.Error("the AP-mode address is not swept")
	}
}

// A /30 has two usable hosts; a /31 has none to sweep.
func TestSubnetHostsSmallPrefixes(t *testing.T) {
	_, n, _ := net.ParseCIDR("10.0.0.0/30")
	if got := subnetHosts(n); len(got) != 2 {
		t.Errorf("/30 = %v, want 2 hosts", got)
	}
}

// A cancelled sweep stops rather than working through the subnet.
func TestDiscoverNetworkHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if _, err := DiscoverNetwork(ctx, 4030); err == nil {
		t.Error("a cancelled sweep reported no error")
	}
	if el := time.Since(start); el > 2*time.Second {
		t.Errorf("cancelled sweep took %s", el)
	}
}

// The same mount reports a different serial on each platform: Linux reads "123456" from the
// descriptor, macOS names the node "usbmodem1234561" — the serial with the CDC interface number
// appended — and that suffix is all this package can recover without IOKit. An entry configured on
// one machine has to bind on the other.
func TestSerialMatchesAcrossPlatforms(t *testing.T) {
	if !serialMatches("1234561", "123456") {
		t.Error("a macOS node serial must match the descriptor serial it was built from")
	}
	if !serialMatches("123456", "1234561") {
		t.Error("the comparison must work in both directions")
	}
	if !serialMatches("123456", "123456") {
		t.Error("identical serials must match")
	}
	for _, tc := range [][2]string{{"123456", "654321"}, {"123456", ""}, {"", "123456"}, {"123456", "7"}} {
		if serialMatches(tc[0], tc[1]) {
			t.Errorf("serialMatches(%q, %q) = true", tc[0], tc[1])
		}
	}
}
