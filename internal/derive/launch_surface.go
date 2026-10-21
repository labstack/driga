package derive

import (
	"iter"
	"path"
	"strings"

	"github.com/labstack/driga/internal/model"
)

// processChain walks only the existing sample. Cycles, zombies and impossible
// parent ages terminate the chain; a newer parent is evidence of PID reuse.
func processChain(byPID map[int32]model.Proc, pid int32) iter.Seq[model.Proc] {
	return func(yield func(model.Proc) bool) {
		seen := map[int32]bool{}
		cur, ok := byPID[pid]
		for ok && cur.PID > 0 && !seen[cur.PID] && !cur.Zombie {
			seen[cur.PID] = true
			if !yield(cur) {
				return
			}
			parent, found := byPID[cur.PPID]
			if !found || (!cur.StartTime.IsZero() && !parent.StartTime.IsZero() && parent.StartTime.After(cur.StartTime)) {
				return
			}
			cur = parent
		}
	}
}

// LaunchSurface names the nearest app bundle in a sampled process's ancestry.
// It performs no process inspection and never infers a product from argv.
func LaunchSurface(byPID map[int32]model.Proc, pid int32) string {
	for p := range processChain(byPID, pid) {
		if bundle := AppBundle(p.Exe); bundle != "" {
			return strings.TrimSuffix(path.Base(bundle), ".app")
		}
	}
	return ""
}
