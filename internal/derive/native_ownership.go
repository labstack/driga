package derive

import (
	"fmt"
	"strings"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/pathx"
	"github.com/labstack/driga/internal/signatures"
)

// SameProcessOwner compares native owner observations. Missing identity is
// unknown, so it cannot exclude a potential owner of the current user's work.
func SameProcessOwner(p, self model.Proc, sourceOS string) (same, known bool) {
	if !p.OwnerKnown || !self.OwnerKnown {
		return false, false
	}
	if sourceOS == "windows" {
		if p.OwnerSID == "" || self.OwnerSID == "" {
			return false, false
		}
		return strings.EqualFold(p.OwnerSID, self.OwnerSID), true
	}
	return p.OwnerUID == self.OwnerUID, true
}

// NativeAgentProcesses includes recognized current interactive/headless agents
// even before their session is stored. Only positively foreign owners are omitted.
func NativeAgentProcesses(procs []model.Proc, self model.Proc, sourceOS string, classifier *Classifier) []model.Proc {
	agents := []model.Proc{}
	for _, p := range procs {
		if p.Zombie {
			continue
		}
		sig, matched := classifier.Match(p)
		role, agent := p.Role, p.Agent
		if matched {
			role = string(sig.Role)
			if sig.Agent != "" {
				agent = sig.Agent
			}
		}
		if agent == "" || role != string(signatures.RoleInteractive) && role != string(signatures.RoleHeadless) {
			continue
		}
		if owned, known := SameProcessOwner(p, self, sourceOS); known && !owned {
			continue
		}
		agents = append(agents, p)
	}
	return agents
}

// NativeAgentOwnershipReason uses one native frame with canonical directories.
// Missing metadata cannot prove that a recognized agent is disconnected.
func NativeAgentOwnershipReason(idx ProcIndex, tree, agents []model.Proc, self model.Proc, sourceOS string) string {
	for _, agent := range agents {
		for _, p := range tree {
			if p.PID == agent.PID {
				return fmt.Sprintf("tree contains or belongs to live agent PID %d", agent.PID)
			}
			for _, ancestor := range idx.Ancestors(p.PID) {
				if ancestor == agent.PID {
					return fmt.Sprintf("tree contains or belongs to live agent PID %d", agent.PID)
				}
			}
		}
		if agent.Cwd == "" {
			return fmt.Sprintf("ownership requires review: working directory unavailable for live agent PID %d; this tree may still be needed", agent.PID)
		}
		for _, p := range tree {
			if p.Cwd == "" {
				return fmt.Sprintf("ownership requires review: working directory unavailable while live agent PID %d may need this tree", agent.PID)
			}
			if pathx.RecordedWithin(p.Cwd, agent.Cwd) || pathx.RecordedWithin(agent.Cwd, p.Cwd) {
				if _, known := SameProcessOwner(agent, self, sourceOS); !known {
					return fmt.Sprintf("ownership requires review: process owner unavailable for live agent PID %d in an overlapping directory", agent.PID)
				}
				return fmt.Sprintf("ownership requires review: live agent PID %d in an overlapping directory may still need this tree", agent.PID)
			}
		}
	}
	return ""
}
