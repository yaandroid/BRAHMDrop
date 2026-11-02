// Package netutil helps find the machine's LAN addresses.
package netutil

import (
	"net"
	"sort"
)

// LocalIPs returns usable non-loopback IPv4 addresses, best (LAN) first.
func LocalIPs() []string {
	var out []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	type scored struct {
		ip    string
		score int
	}
	var list []scored
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.To4() == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			s := 0
			if ip.IsPrivate() {
				s = 10 // prefer typical LAN ranges
			}
			list = append(list, scored{ip.String(), s})
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].score > list[j].score })
	for _, s := range list {
		out = append(out, s.ip)
	}
	return out
}

// PrimaryIP returns the single best LAN IP, or "127.0.0.1" if none.
func PrimaryIP() string {
	ips := LocalIPs()
	if len(ips) == 0 {
		return "127.0.0.1"
	}
	return ips[0]
}
