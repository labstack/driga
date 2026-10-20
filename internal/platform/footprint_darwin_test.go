package platform

import (
	"os"
	"testing"
)

func TestPhysicalFootprintSelf(t *testing.T) {
	got, err := physicalFootprint(int32(os.Getpid()))
	if err != nil || got == 0 {
		t.Fatalf("own physical footprint: %d %v", got, err)
	}
	if _, err := physicalFootprint(-1); err == nil {
		t.Fatal("invalid PID accepted")
	}
}
func TestDarwinPressureLevels(t *testing.T) {
	for n, want := range map[uint32]string{0: "unknown", 1: "normal", 2: "warning", 4: "critical", 7: "unknown"} {
		if got := pressureLevelDarwin(n); got != want {
			t.Fatalf("%d: %q", n, got)
		}
	}
}
