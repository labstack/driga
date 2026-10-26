package cli

import (
	"strings"
	"testing"
)

// The page acts on this machine, so it is served on loopback only.
func TestDevServeRefusesNonLoopback(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:47801", "192.168.1.2:47801", "example.com:80", "47801"} {
		err := (DevServeCmd{Addr: addr}).Run(&Globals{})
		if err == nil || !strings.Contains(err.Error(), "addr must") {
			t.Errorf("%s: %v", addr, err)
		}
	}
}
