package cli

import (
	"os"
	"runtime"
	"runtime/debug"
)

// collectorMemoryLimit is the collector's soft Go memory limit. Its live heap
// is a few MB; without a limit the runtime retains enough freed memory to push
// RSS past the 50 MB footprint budget during the initial import.
const collectorMemoryLimit = 32 << 20

// collectorGCPercent halves the heap headroom between collections. The live
// heap is small, so the extra GC work is negligible; it keeps a serving
// collector's peak RSS inside the budget.
const collectorGCPercent = 50

// applyCollectorMemoryLimit sets the soft limit and GC percent unless the user
// chose them via GOMEMLIMIT or GOGC. The limit is a target, not a cap: the GC
// works harder near it but never fails allocations.
func applyCollectorMemoryLimit() {
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(collectorMemoryLimit)
	}
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(collectorGCPercent)
	}
}

// applyCollectorParallelism reduces GC coordination across Ps for the daemon's
// small heap and high allocation churn. An explicit user GOMAXPROCS wins.
func applyCollectorParallelism() {
	if _, set := os.LookupEnv("GOMAXPROCS"); !set {
		runtime.GOMAXPROCS(min(2, runtime.NumCPU()))
	}
}
