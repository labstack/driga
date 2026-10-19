package cli

import (
	"os"
	"runtime/debug"
)

// collectorMemoryLimit is the collector's soft Go memory limit. Its live heap
// is a few MB; without a limit the runtime retains enough freed memory to push
// RSS past the 50 MB footprint budget during the initial import.
const collectorMemoryLimit = 32 << 20

// applyCollectorMemoryLimit sets the soft limit unless the user chose one via
// GOMEMLIMIT. It is a target, not a cap: the GC works harder near it but never
// fails allocations.
func applyCollectorMemoryLimit() {
	if os.Getenv("GOMEMLIMIT") != "" {
		return
	}
	debug.SetMemoryLimit(collectorMemoryLimit)
}
