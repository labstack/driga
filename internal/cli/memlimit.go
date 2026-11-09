package cli

import (
	"os"
	"runtime"
	"runtime/debug"
)

// collectorMemoryLimit is the collector's soft Go memory limit. Its live heap
// is a few MB; without a limit the runtime retains enough freed memory to push
// its footprint past the 50 MB budget during the initial import.
const collectorMemoryLimit = 32 << 20

// collectorGCPercent halves the heap headroom between collections. The live
// heap is small, so the extra GC work is negligible; it keeps a serving
// collector's peak RSS inside the budget.
const collectorGCPercent = 50

// applyCollectorMemoryLimit sets the soft limit and GC percent unless the user
// chose them via GOMEMLIMIT or GOGC. The limit is a target, not a cap: the GC
// works harder near it but never fails allocations. The returned function
// restores settings changed here when an embedded command returns.
func applyCollectorMemoryLimit() func() {
	memory, gc := os.Getenv("GOMEMLIMIT") == "", os.Getenv("GOGC") == ""
	var previousMemory int64
	var previousGC int
	if memory {
		previousMemory = debug.SetMemoryLimit(collectorMemoryLimit)
	}
	if gc {
		previousGC = debug.SetGCPercent(collectorGCPercent)
	}
	return func() {
		if gc {
			debug.SetGCPercent(previousGC)
		}
		if memory {
			debug.SetMemoryLimit(previousMemory)
		}
	}
}

// applyCollectorParallelism reduces GC coordination across Ps for the daemon's
// small heap and high allocation churn. An explicit user GOMAXPROCS wins.
func applyCollectorParallelism() func() {
	if _, set := os.LookupEnv("GOMAXPROCS"); !set {
		previous := runtime.GOMAXPROCS(min(2, runtime.NumCPU()))
		return func() { runtime.GOMAXPROCS(previous) }
	}
	return func() {}
}
