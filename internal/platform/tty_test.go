//go:build darwin || linux

package platform

import "testing"

func TestControllingTTYFromStat(t *testing.T) {
	for _, tc := range []struct {
		s    string
		want uint64
	}{{"12 (a (weird) comm) S 1 2 3 34817 0", 34817}, {"12 (x) S 1 2 3 0 0", 0}, {"bad", 0}, {"12 (x) S 1 2 3 -1 0", 0}} {
		if got := controllingTTYDevice([]byte(tc.s)); got != tc.want {
			t.Fatal(tc, got)
		}
	}
}
