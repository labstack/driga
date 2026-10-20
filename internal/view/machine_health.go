package view

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/signatures"
)

// MachineAttribution records the proof, never a guessed executable match.
type MachineAttribution struct {
	SessionID int64  `json:"session_id"`
	Project   string `json:"project"`
	Proof     string `json:"proof"`
}
type MachineProcess struct {
	model.Proc
	Memory      uint64              `json:"memory_bytes"`
	Attribution *MachineAttribution `json:"attribution,omitempty"`
}
type MachineApp struct {
	ID            string           `json:"id"`
	Name          string           `json:"name"`
	Memory        uint64           `json:"memory_bytes"`
	MemoryMeasure string           `json:"memory_measure"`
	CPUPercent    float64          `json:"cpu_percent"`
	Processes     []MachineProcess `json:"processes"`
}

func machineApps(procs []model.Proc, sessions []model.Session) ([]MachineApp, []MachineApp) {
	byPID := make(map[int32]model.Proc, len(procs))
	for _, p := range procs {
		byPID[p.PID] = p
	}
	live := map[int32]model.Session{}
	for _, s := range sessions {
		p, ok := byPID[s.PID]
		if !ok || s.ID == 0 || s.EndedAt != nil || s.Label == "dead" || s.Hosted || s.ProcStart.IsZero() || !derive.SameStart(p.StartTime, s.ProcStart) {
			continue
		}
		live[s.PID] = s
	}
	apps := map[string]*MachineApp{}
	for _, p := range procs {
		if p.Zombie {
			continue
		}
		row := MachineProcess{Proc: p}
		row.Memory, row.MemoryMeasure = derive.ProcessMemory(p)
		// Stored orphan ancestry is not evidence of a currently live session.
		row.SessionID = 0
		bundle := ""
		cur := p
		seen := map[int32]bool{}
		for cur.PID > 0 && !seen[cur.PID] && !cur.Zombie {
			seen[cur.PID] = true
			if bundle == "" {
				bundle = derive.AppBundle(cur.Exe)
			}
			if s, ok := live[cur.PID]; ok && row.Attribution == nil {
				row.Attribution = &MachineAttribution{SessionID: s.ID, Project: path.Base(s.Cwd), Proof: "parent_chain"}
			}
			parent, ok := byPID[cur.PPID]
			// Reject impossible links left by PID reuse in a non-atomic scan.
			if !ok || (!cur.StartTime.IsZero() && !parent.StartTime.IsZero() && parent.StartTime.After(cur.StartTime)) {
				break
			}
			cur = parent
		}
		if row.Attribution == nil && p.Cwd != "" && !p.StartTime.IsZero() {
			var chosen *model.Session
			for _, s := range live {
				if s.StartedAt.IsZero() || !p.StartTime.After(s.StartedAt) || !within(p.Cwd, s.Cwd) {
					continue
				}
				// Multiple sessions in the same best-matching project cannot prove which
				// session started the process. Prefer a uniquely more specific project.
				if chosen == nil || len(s.Cwd) > len(chosen.Cwd) {
					copy := s
					chosen = &copy
				} else if len(s.Cwd) == len(chosen.Cwd) {
					chosen.ID = 0
				}
			}
			if chosen != nil && chosen.ID != 0 {
				row.Attribution = &MachineAttribution{SessionID: chosen.ID, Project: path.Base(chosen.Cwd), Proof: "cwd_started_after"}
			}
		}
		if row.Attribution != nil {
			row.SessionID = row.Attribution.SessionID
		}
		id, name := bundle, strings.TrimSuffix(path.Base(bundle), ".app")
		if bundle == "" {
			name = signatures.Builtin().ProcessName(p.Cmdline, p.Exe)
			id = "exe:" + name
		}
		a := apps[id]
		if a == nil {
			a = &MachineApp{ID: id, Name: name, MemoryMeasure: row.MemoryMeasure, Processes: []MachineProcess{}}
			apps[id] = a
		}
		if a.MemoryMeasure != row.MemoryMeasure {
			a.MemoryMeasure = "mixed"
		}
		a.Memory += row.Memory
		a.CPUPercent += p.CPUPercent
		a.Processes = append(a.Processes, row)
	}
	mem := make([]MachineApp, 0, len(apps))
	for _, a := range apps {
		sort.Slice(a.Processes, func(i, j int) bool {
			if a.Processes[i].Memory != a.Processes[j].Memory {
				return a.Processes[i].Memory > a.Processes[j].Memory
			}
			return a.Processes[i].PID < a.Processes[j].PID
		})
		mem = append(mem, *a)
	}
	sort.Slice(mem, func(i, j int) bool {
		if mem[i].Memory != mem[j].Memory {
			return mem[i].Memory > mem[j].Memory
		}
		return mem[i].ID < mem[j].ID
	})
	cpu := append([]MachineApp{}, mem...)
	sort.Slice(cpu, func(i, j int) bool {
		if cpu[i].CPUPercent != cpu[j].CPUPercent {
			return cpu[i].CPUPercent > cpu[j].CPUPercent
		}
		return cpu[i].ID < cpu[j].ID
	})
	return mem[:min(10, len(mem))], cpu[:min(10, len(cpu))]
}

// MCPPileupText supplies the same evidence and read-only advice to CLI/TUI.
func MCPPileupText(p derive.MCPPileup) string {
	return fmt.Sprintf("%s: %d processes · %s (%s)\n%s\nHost: %s (PID %d) · age %s\nHost started: %s\nHost project: %s\nOldest: %s\nNewest: %s\n%s",
		p.Name, p.Count, humanBytes(p.Memory), p.MemoryMeasure, MCPCommandDetails(p.MCPCommand),
		p.Host.Name, p.Host.PID, age(p.Host.Age), p.Host.StartedAt.Format(time.RFC3339), p.Host.Cwd,
		p.OldestStart.Format(time.RFC3339), p.NewestStart.Format(time.RFC3339), strings.ReplaceAll(p.Advice, "; ", ";\n"))
}

// MCPCommandSummary is shared by CLI, TUI and web roll-ups.
func MCPCommandSummary(c derive.MCPCommand) string {
	text := fmt.Sprintf("%s  %d servers · %s", c.Name, c.Count, humanBytes(c.Memory))
	if marker := MCPCommandTrendMarker(c); marker != "" {
		text += " · " + marker
	}
	text += fmt.Sprintf(" · %d hosts · %d duplicates", c.HostCount, c.DuplicateCount)
	if c.IncludesLauncher {
		text += " · includes launcher"
	}
	return text
}

func MCPCommandTrendMarker(c derive.MCPCommand) string {
	if len(c.Trends) == 0 {
		return ""
	}
	chosen := c.Trends[0]
	for _, t := range c.Trends {
		if t.State == "leaking" {
			chosen = t
			break
		}
		if t.State == "churning" {
			chosen = t
		}
	}
	return MCPTrendMarker(chosen)
}

func MCPTrendMarker(t derive.MCPTrend) string {
	if t.State == "leaking" {
		return fmt.Sprintf("leaking ↑ %.2g/h since %s", t.RatePerHour, t.FirstSample.In(time.Local).Format("15:04"))
	}
	return t.State
}

func MCPCommandDetails(c derive.MCPCommand) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Memory measure: %s", c.MemoryMeasure)
	if c.IncludesLauncher {
		b.WriteString(" · includes launcher")
	}
	commands := c.Commands
	if len(commands) == 0 {
		commands = []string{c.Command}
	}
	for _, command := range commands {
		fmt.Fprintf(&b, "\nCommand: %s", command)
	}
	if len(c.Installs) > 0 {
		fmt.Fprintf(&b, "\nInstalls:\n  %s", strings.Join(c.Installs, "\n  "))
	}
	for _, t := range c.Trends {
		fmt.Fprintf(&b, "\n%s PID %d: %s · started %d · ended %d · net %+d · %.2g/h\nObserved: %s to %s", t.Host.Name, t.Host.PID, MCPTrendMarker(t), t.StartedCount, t.EndedCount, t.NetGrowth, t.RatePerHour, t.FirstSample.Format(time.RFC3339), t.LastSample.Format(time.RFC3339))
		if !t.IdentityComplete {
			b.WriteString(" · incomplete process start identities")
		}
	}
	return b.String()
}

func MCPCommandText(c derive.MCPCommand) string {
	return MCPCommandSummary(c) + "\n" + MCPCommandDetails(c)
}

// machineProjectSessions uses known checkout roots so a session launched in a
// subdirectory can prove a later process in a sibling directory. It never
// widens the match beyond a stored repo/worktree containing the session cwd.
func machineProjectSessions(sessions []model.Session, repos []model.Repo, worktrees []model.Worktree) []model.Session {
	for i, s := range sessions {
		if s.RepoID == nil {
			continue
		}
		root := ""
		for _, r := range repos {
			if r.ID == *s.RepoID && within(s.Cwd, r.Path) {
				root = r.Path
			}
		}
		for _, w := range worktrees {
			if w.RepoID == *s.RepoID && within(s.Cwd, w.Path) && len(w.Path) > len(root) {
				root = w.Path
			}
		}
		if root != "" {
			sessions[i].Cwd = root
		}
	}
	return sessions
}
