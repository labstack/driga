package cli

import (
	"math"
	"os"
	"runtime"
	"runtime/debug"
	"testing"
)

func TestCollectorParallelismDefault(t *testing.T) {
	old := runtime.GOMAXPROCS(4)
	t.Cleanup(func() { runtime.GOMAXPROCS(old) })
	t.Setenv("GOMAXPROCS", "")
	if err := os.Unsetenv("GOMAXPROCS"); err != nil {
		t.Fatal(err)
	}

	applyCollectorParallelism()
	if got, want := runtime.GOMAXPROCS(0), min(2, runtime.NumCPU()); got != want {
		t.Fatalf("default GOMAXPROCS = %d, want %d", got, want)
	}
}

func TestCollectorParallelismUserEnv(t *testing.T) {
	old := runtime.GOMAXPROCS(3)
	t.Cleanup(func() { runtime.GOMAXPROCS(old) })
	for _, value := range []string{"8", ""} {
		t.Run("GOMAXPROCS="+value, func(t *testing.T) {
			t.Setenv("GOMAXPROCS", value)
			applyCollectorParallelism()
			if got := runtime.GOMAXPROCS(0); got != 3 {
				t.Fatalf("user GOMAXPROCS must win, value changed to %d", got)
			}
		})
	}
}

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

// Measured on a serving collector with two viewers: GOGC=50 keeps peak RSS
// about 4 MB lower than the default at a negligible GC cost (live heap ~8 MB).
func TestCollectorGCPercentDefault(t *testing.T) {
	old := debug.SetGCPercent(100)
	t.Cleanup(func() { debug.SetGCPercent(old) })

	t.Setenv("GOGC", "")
	applyCollectorMemoryLimit()
	if got := debug.SetGCPercent(100); got != collectorGCPercent {
		t.Fatalf("default GC percent = %d, want %d", got, collectorGCPercent)
	}

	t.Setenv("GOGC", "200")
	applyCollectorMemoryLimit()
	if got := debug.SetGCPercent(100); got != 100 {
		t.Fatalf("user GOGC must win, GC percent changed to %d", got)
	}
}
