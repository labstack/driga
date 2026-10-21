package fleet_test

import (
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/fleet"
)

func TestDeviceIDRoundTripAndCheckDigit(t *testing.T) {
	for _, der := range [][]byte{nil, []byte("certificate"), []byte("other certificate")} {
		id := fleet.DeviceID(der)
		groups := strings.Split(id, "-")
		if len(groups) != 8 {
			t.Fatalf("wrong groups: %q", id)
		}
		for _, g := range groups {
			if len(g) != 7 {
				t.Fatalf("wrong group: %q", g)
			}
		}
		got, err := fleet.ParseDeviceID(id)
		if err != nil || got != sha256.Sum256(der) {
			t.Fatalf("round trip %q: %v", id, err)
		}
		got, err = fleet.ParseDeviceID(strings.ToLower(id))
		if err != nil || got != sha256.Sum256(der) {
			t.Fatal("lowercase rejected", err)
		}
		for i := range id {
			if id[i] == '-' {
				continue
			}
			for _, ch := range "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567" {
				if byte(ch) == id[i] {
					continue
				}
				bad := []byte(id)
				bad[i] = byte(ch)
				if _, err := fleet.ParseDeviceID(string(bad)); err == nil {
					t.Fatalf("accepted flipped char %d: %s", i, bad)
				}
			}
		}
	}
	for _, bad := range []string{"", "abc", strings.Repeat("A", 56), "!" + fleet.DeviceID(nil)[1:], fleet.DeviceID(nil) + "="} {
		if _, err := fleet.ParseDeviceID(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}
