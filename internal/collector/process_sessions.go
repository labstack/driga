package collector

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
)

// A recognized launcher with a same-agent child is infrastructure, not another
// conversation. Native parent/child agents remain separate sessions.
func agentLaunchers(procs []model.Proc) map[int32]bool {
	byPID := make(map[int32]model.Proc, len(procs))
	for _, p := range procs {
		byPID[p.PID] = p
	}
	aliases := make(map[int32]bool)
	for _, child := range procs {
		parent, ok := byPID[child.PPID]
		if !ok || parent.Agent == "" || parent.Agent != child.Agent || parent.StartTime.IsZero() || child.StartTime.Before(parent.StartTime) || (child.Role != "interactive" && child.Role != "headless") {
			continue
		}
		exe := strings.ToLower(strings.ReplaceAll(parent.Exe, `\`, "/"))
		base := exe[strings.LastIndex(exe, "/")+1:]
		if strings.Contains(exe, "/scoop/shims/") || strings.Contains(exe, "/bin/copilot-shim/") || parent.Agent == "copilot" && (base == "node" || base == "node.exe") {
			aliases[parent.PID] = true
		}
	}
	return aliases
}

func processSessions(ss []model.Session, procs []model.Proc, d Deps) []model.Session {
	launchers := agentLaunchers(procs)
	for _, proc := range procs {
		switch proc.Agent {
		case "claude", "codex", "copilot":
			if !d.Cfg.Adapters[proc.Agent].Enabled {
				continue
			}
		case "agy":
		default:
			continue
		}
		if launchers[proc.PID] || proc.StartTime.IsZero() || (proc.Role != "interactive" && proc.Role != "headless") || !d.Plat.Alive(proc.PID, proc.StartTime) {
			continue
		}
		linked := false
		for _, s := range ss {
			if s.Adapter == proc.Agent && !strings.HasPrefix(s.ExternalID, "pid:") && s.PID == proc.PID && !s.ProcStart.IsZero() && platform.SameStart(proc.StartTime, s.ProcStart) {
				linked = true
				break
			}
		}
		if linked {
			continue
		}
		id := fmt.Sprintf("pid:%d:%d", proc.PID, proc.StartTime.UnixNano())
		i := -1
		for j, s := range ss {
			if s.Adapter == proc.Agent && s.ExternalID == id {
				i = j
				break
			}
		}
		if i < 0 {
			i = len(ss)
			ss = append(ss, model.Session{})
		}
		s := &ss[i]
		s.SessionObs = model.SessionObs{Adapter: proc.Agent, ExternalID: id, Origin: proc.Role, PID: proc.PID, ProcStart: proc.StartTime, StartedAt: proc.StartTime, Cwd: proc.Cwd}
		if proc.Agent != "agy" && proc.Cwd != "" {
			s.Title = filepath.Base(proc.Cwd)
		}
		s.EndedAt = nil
	}
	return ss
}

// Retire provisional identities once a registry, rollout or hook identifies the
// conversation, and remove launcher rows left by an older collector version.
func retireProcessAliases(ss []model.Session, procs []model.Proc, now time.Time) {
	launchers := agentLaunchers(procs)
	for i := range ss {
		p := &ss[i]
		if !strings.HasPrefix(p.ExternalID, "pid:") || p.PID == 0 {
			continue
		}
		retire := launchers[p.PID]
		for _, s := range ss {
			if !strings.HasPrefix(s.ExternalID, "pid:") && s.Adapter == p.Adapter && s.PID == p.PID && !s.ProcStart.IsZero() && platform.SameStart(p.ProcStart, s.ProcStart) {
				retire = true
				break
			}
		}
		if retire {
			p.PID, p.TurnOpen = 0, false
			at := now
			p.EndedAt = &at
		}
	}
}
