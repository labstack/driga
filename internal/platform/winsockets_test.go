package platform

import (
	"encoding/hex"
	"testing"

	"github.com/labstack/driga/internal/model"
)

// Fixed DWORD-layout byte fixtures: count followed by MIB_*ROW_OWNER_PID. Addresses
// and ports stay in network byte order; state, scope and PID are little endian.
func TestWindowsSocketTableBuffers(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		family    int
		udp       bool
		pid       int32
		state     model.PeerState
	}{
		{"tcp4 listen", "01000000 02000000 00000000 1f900000 00000000 00000000 64000000", 2, false, 100, model.PeerListening},
		{"tcp4 established", "01000000 05000000 7f000001 c0000000 7f000001 1f900000 65000000", 2, false, 101, model.PeerConnected},
		{"tcp4 closed", "01000000 01000000 00000000 00000000 00000000 00000000 64000000", 2, false, 100, model.PeerNone},
		{"tcp6 listen", "01000000 00000000000000000000000000000000 00000000 1f900000 00000000000000000000000000000000 00000000 00000000 02000000 66000000", 23, false, 102, model.PeerListening},
		{"udp4 bound", "01000000 00000000 00350000 67000000", 2, true, 103, model.PeerConnected},
		{"udp6 bound", "01000000 00000000000000000000000000000000 00000000 00350000 68000000", 23, true, 104, model.PeerConnected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var compact string
			for _, c := range tc.raw {
				if c != ' ' {
					compact += string(c)
				}
			}
			b, err := hex.DecodeString(compact)
			if err != nil {
				t.Fatal(err)
			}
			got, err := parseWindowsSocketTable(b, tc.family, tc.udp, []int32{tc.pid, 999})
			if err != nil || got[tc.pid] != tc.state || got[999] != model.PeerNone || len(got) != 2 {
				t.Fatalf("%v %v", got, err)
			}
			if _, err := parseWindowsSocketTable(b[:len(b)-1], tc.family, tc.udp, nil); err == nil {
				t.Fatal("truncation accepted")
			}
		})
	}
	for _, b := range [][]byte{nil, {255, 255, 255, 255}, {1, 0, 0, 0}} {
		if _, err := parseWindowsSocketTable(b, 2, false, nil); err == nil {
			t.Fatal("bad buffer accepted")
		}
	}
}
