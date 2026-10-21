package fleet

import (
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
)

const fleetPort = "7411"

var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// CheckListen resolves an explicit IP or interface name into a TCP address.
// A missing port uses 7411; port zero is preserved. Hostnames are not resolved.
// Wildcard and public IPs require allowAny, including interface addresses.
func CheckListen(addr string, allowAny bool) (string, error) {
	if addr == "" {
		return "", fmt.Errorf("fleet listen requires an explicit address")
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host, port = addr, fleetPort
		if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
			host = host[1 : len(host)-1]
		}
		if strings.Contains(host, ":") {
			if _, err := netip.ParseAddr(host); err != nil {
				return "", fmt.Errorf("invalid fleet listen address %q", addr)
			}
		}
	}
	if host == "" || port == "" {
		return "", fmt.Errorf("fleet listen requires an explicit address and port")
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 0 || p > 65535 {
		return "", fmt.Errorf("invalid fleet listen port %q", port)
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		check := ip.Unmap()
		if !allowAny && !privateListenIP(check) {
			return "", fmt.Errorf("fleet listen address %s requires allow_any_address", host)
		}
		return net.JoinHostPort(ip.String(), strconv.Itoa(p)), nil
	}
	iface, addrs, err := listenInterface(host)
	if err != nil {
		return "", fmt.Errorf("fleet listen must name an IP address or interface: %w", err)
	}
	var candidates []netip.Addr
	for _, addr := range addrs {
		prefix, err := netip.ParsePrefix(addr.String())
		if err != nil {
			continue
		}
		ip := prefix.Addr().Unmap()
		if !ip.IsUnspecified() && !ip.IsMulticast() && (allowAny || privateListenIP(ip)) {
			candidates = append(candidates, ip)
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("fleet listen interface %q has no permitted unicast address; public addresses require allow_any_address", host)
	}
	// Prefer IPv4, then numerical order, for repeatable interface resolution.
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Less(candidates[j]) })
	ip := candidates[0]
	if ip.Is6() && ip.IsLinkLocalUnicast() {
		ip = ip.WithZone(iface.Name)
	}
	return net.JoinHostPort(ip.String(), strconv.Itoa(p)), nil
}

var listenInterface = func(name string) (*net.Interface, []net.Addr, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return nil, nil, err
	}
	addrs, err := iface.Addrs()
	return iface, addrs, err
}

func privateListenIP(ip netip.Addr) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || cgnat.Contains(ip)
}
