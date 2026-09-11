package am5

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

// Finding an AM-series mount on the network.
//
// # Why a sweep, and not something better
//
// The mount advertises nothing. It answers no mDNS, no broadcast, and it will not tell you the
// address a router gave it — INDIGO's driver makes the operator type one, and INDI's hardcodes
// 192.168.4.1 with a subnet sweep behind it. So there are two addresses worth knowing and only
// one of them is guessable: in the mount's own access-point mode it is always 192.168.4.1, and on
// a home network it is whatever DHCP handed out. The sweep exists for the second.
//
// It is a TCP connect to one port on each host of the local subnets, then the same :GVP# question
// the USB path asks. A host that refuses the connection costs nothing; a host that ignores it
// costs the dial timeout. Nothing is sent to a host that does not accept a connection on 4030,
// which keeps this from being a port scan of the neighbourhood.
//
// # What it will not do
//
// Sweep a network too large to sweep politely. A /24 is 254 hosts and finishes in about a second
// at this concurrency; a /16 is 65,000 and would take minutes while looking, to anything watching
// the network, exactly like reconnaissance. Subnets wider than /22 are skipped, and an operator
// on one types the address instead.

const (
	sweepTimeout     = 400 * time.Millisecond // per-host TCP connect
	sweepConcurrency = 64                     // hosts dialled at once
	sweepMinPrefix   = 22                     // widest subnet worth sweeping (1022 hosts)
)

// DiscoverNetwork lists the AM-series mounts reachable on this machine's local subnets.
//
// port is the TCP port to look on; 0 uses 4030, which is the only port ZWO's firmware serves.
func DiscoverNetwork(ctx context.Context, port int) ([]Discovered, error) {
	if port <= 0 {
		port = DefaultTCPPort
	}
	hosts, err := sweepHosts()
	if err != nil {
		return nil, err
	}
	found := make(chan Discovered, len(hosts))
	sem := make(chan struct{}, sweepConcurrency)
	var wg sync.WaitGroup
	for _, h := range hosts {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			addr := net.JoinHostPort(host, fmt.Sprint(port))
			// The cheap test first: a host with nothing listening on this port refuses at once,
			// and most of a subnet is that. Only the few that accept are asked anything.
			c, err := net.DialTimeout("tcp", addr, sweepTimeout)
			if err != nil {
				return
			}
			c.Close()
			if product, version, ok := probeAddr(addr); ok {
				found <- Discovered{Addr: addr, Product: product, Version: version}
			}
		}(h)
	}
	wg.Wait()
	close(found)
	var out []Discovered
	for d := range found {
		out = append(out, d)
	}
	return out, ctx.Err()
}

// probeAddr asks a listening address what it is, closing the connection again.
//
// Something answers on a port; that is not the same as a mount. A print server, a serial bridge,
// another instrument's control port — each accepts a connection and none of them is an AM5, so
// the product string decides, exactly as it does over USB.
func probeAddr(addr string) (product, version string, ok bool) {
	m, err := Dial(addr)
	if err != nil {
		return "", "", false
	}
	defer m.Close()
	p, err := m.Product()
	if err != nil || !isAMProduct(p) {
		return "", "", false
	}
	v, err := m.Firmware()
	if err != nil {
		v = ""
	}
	return p, v, true
}

// sweepHosts lists every address on this machine's local IPv4 subnets, excluding our own and the
// subnet's network and broadcast addresses.
func sweepHosts() ([]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil {
				continue
			}
			ones, bits := ipnet.Mask.Size()
			if bits != 32 || ones < sweepMinPrefix || ones >= 31 {
				continue // too wide to sweep politely, or a point-to-point link with no hosts
			}
			for _, h := range subnetHosts(ipnet) {
				if h == ipnet.IP.String() || seen[h] {
					continue // ourselves, or an address two interfaces share
				}
				seen[h] = true
				out = append(out, h)
			}
		}
	}
	return out, nil
}

// subnetHosts enumerates the usable addresses of an IPv4 subnet.
func subnetHosts(n *net.IPNet) []string {
	ip := n.IP.Mask(n.Mask).To4()
	if ip == nil {
		return nil
	}
	mask := net.IP(n.Mask).To4()
	start := uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
	m := uint32(mask[0])<<24 | uint32(mask[1])<<16 | uint32(mask[2])<<8 | uint32(mask[3])
	end := start | ^m
	out := make([]string, 0, end-start)
	for v := start + 1; v < end; v++ { // skip the network and broadcast addresses
		out = append(out, net.IPv4(byte(v>>24), byte(v>>16), byte(v>>8), byte(v)).String())
	}
	return out
}
