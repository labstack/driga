package fleet_test

import (
	"net"
	"testing"

	"github.com/labstack/driga/internal/fleet"
)

func TestCheckListen(t *testing.T) {
	for _, tc := range []struct {
		addr      string
		allow, ok bool
		want      string
	}{
		{"0.0.0.0:7411", false, false, ""}, {"[::]:7411", false, false, ""},
		{"10.0.0.5:7411", false, true, "10.0.0.5:7411"}, {"172.16.1.2:7411", false, true, "172.16.1.2:7411"},
		{"192.168.1.2:7411", false, true, "192.168.1.2:7411"}, {"100.64.1.2:7411", false, true, "100.64.1.2:7411"},
		{"fd00::1", false, true, "[fd00::1]:7411"}, {"[fd00::1]:7411", false, true, "[fd00::1]:7411"},
		{"[fe80::1%lo0]:7411", false, true, "[fe80::1%lo0]:7411"}, {"169.254.1.2:7411", false, true, "169.254.1.2:7411"},
		{"8.8.8.8:7411", false, false, ""}, {"8.8.8.8:7411", true, true, "8.8.8.8:7411"},
		{"0.0.0.0:7411", true, true, "0.0.0.0:7411"}, {"[::]:7411", true, true, "[::]:7411"},
		{"127.0.0.1:0", false, true, "127.0.0.1:0"}, {"127.0.0.1", false, true, "127.0.0.1:7411"},
		{"", false, false, ""}, {":7411", true, false, ""}, {"127.0.0.1:-1", false, false, ""},
		{"127.0.0.1:65536", false, false, ""}, {"127.0.0.1:http", false, false, ""}, {"224.0.0.1:7411", false, false, ""},
		{"100.63.255.255:7411", false, false, ""}, {"100.128.0.0:7411", false, false, ""}, {"does-not-exist:7411", false, false, ""},
	} {
		t.Run(tc.addr, func(t *testing.T) {
			got, err := fleet.CheckListen(tc.addr, tc.allow)
			if (err == nil) != tc.ok || got != tc.want {
				t.Fatalf("got %q, %v; want %q success=%v", got, err, tc.want, tc.ok)
			}
		})
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback == 0 {
			continue
		}
		got, err := fleet.CheckListen(iface.Name+":0", false)
		if err != nil {
			t.Fatal(err)
		}
		host, port, err := net.SplitHostPort(got)
		if err != nil || port != "0" || !net.ParseIP(host).IsLoopback() {
			t.Fatal(got, err)
		}
		return
	}
	t.Fatal("no loopback interface found")
}
