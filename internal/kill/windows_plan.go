package kill

import (
	"fmt"

	"github.com/labstack/driga/internal/model"
)

// Windows never reparents children. Surviving members must retain their exact
// identity and creator PID; an extra child of ANY member aborts escalation,
// including after an intermediate parent has already exited.
func windowsTreeChanged(targets, current []model.Proc) string {
	want := make(map[int32]model.Proc, len(targets))
	for _, p := range targets {
		want[p.PID] = p
	}
	for _, p := range current {
		if old, ok := want[p.PID]; ok {
			if p.StartTime.IsZero() || !p.StartTime.Equal(old.StartTime) || p.PPID != old.PPID {
				return fmt.Sprintf("tree identity changed at PID %d", p.PID)
			}
		} else if _, child := want[p.PPID]; child {
			return "orphan descendant set changed"
		}
	}
	return ""
}
