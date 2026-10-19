package cli

import (
	"math"
	"runtime/debug"
	"testing"
)

// The collector is a long-lived daemon with a ~5 MB live heap; without a soft
// limit the runtime keeps far more resident than the 50 MB budget allows.
func TestCollectorMemoryLimitDefault(t *testing.T) {
	old := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(old) })

	t.Setenv("GOMEMLIMIT", "")
	debug.SetMemoryLimit(math.MaxInt64)
	applyCollectorMemoryLimit()
	if got := debug.SetMemoryLimit(-1); got != collectorMemoryLimit {
		t.Fatalf("default limit = %d, want %d", got, collectorMemoryLimit)
	}

	t.Setenv("GOMEMLIMIT", "200MiB")
	debug.SetMemoryLimit(math.MaxInt64)
	applyCollectorMemoryLimit()
	if got := debug.SetMemoryLimit(-1); got != math.MaxInt64 {
		t.Fatalf("user GOMEMLIMIT must win, limit changed to %d", got)
	}
}
