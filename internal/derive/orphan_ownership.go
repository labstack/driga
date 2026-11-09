package derive

import (
	"fmt"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/pathx"
)

// OrphanOwnershipReason explains why a currently live session may still need a
// helper tree. Idle CPU and absent peers cannot prove that a reusable broker is
// disconnected: a session can reconnect when its next turn starts.
// Callers must supply a current process snapshot and current session records.
func OrphanOwnershipReason(idx ProcIndex, sessions []model.Session, rootPID int32) string {
	root, ok := idx.Get(rootPID)
	if !ok {
		return "ownership cannot be checked: process exited"
	}
	live := familyLivePIDs(sessions, idx)
	tree := append([]model.Proc{root}, idx.Descendants(rootPID)...)
	for _, p := range tree {
		if owner, ok := live[p.PID]; ok {
			return fmt.Sprintf("tree contains live session %d", owner)
		}
		for _, ancestor := range idx.Ancestors(p.PID) {
			if owner, ok := live[ancestor]; ok {
				return fmt.Sprintf("tree belongs to live session %d", owner)
			}
		}
	}
	for _, s := range sessions {
		_, processLive := live[s.PID]
		if !familySessionLive(s) || !processLive && !(s.PID == 0 && s.Hosted) || s.Cwd == "" {
			continue
		}
		for _, p := range tree {
			if p.Cwd == "" {
				return fmt.Sprintf("ownership requires review: working directory unavailable for pid %d while session %d is live", p.PID, s.ID)
			}
			if p.Cwd != "" && pathx.Equal(p.Cwd, s.Cwd) {
				return fmt.Sprintf("ownership requires review: live session %d in same directory may still need this tree", s.ID)
			}
		}
	}
	return ""
}
