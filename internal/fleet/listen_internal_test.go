package fleet

import (
	"net"
	"strings"
	"testing"
)

func TestListenInterfaceNamePublicRefused(t *testing.T) {
	old := listenInterface
	t.Cleanup(func() { listenInterface = old })
	addresses := []net.Addr{&net.IPNet{IP: net.ParseIP("8.8.8.8"), Mask: net.CIDRMask(24, 32)}}
	listenInterface = func(name string) (*net.Interface, []net.Addr, error) {
		return &net.Interface{Name: name}, addresses, nil
	}
	if got, err := CheckListen("eth0:0", false); err == nil || !strings.Contains(err.Error(), "allow_any_address") {
		t.Fatalf("public interface accepted: %q %v", got, err)
	}
	if got, err := CheckListen("eth0:0", true); err != nil || got != "8.8.8.8:0" {
		t.Fatalf("explicit override: %q %v", got, err)
	}
	addresses = append(addresses, &net.IPNet{IP: net.ParseIP("10.0.0.2"), Mask: net.CIDRMask(24, 32)})
	if got, err := CheckListen("eth0:0", false); err != nil || got != "10.0.0.2:0" {
		t.Fatalf("private address not selected: %q %v", got, err)
	}
}
