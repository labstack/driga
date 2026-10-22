package view

import (
	"path"
	"sort"
	"strings"

	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/signatures"
)

type AgentShare struct {
	Memory        uint64  `json:"memory_bytes"`
	LikelyMemory  uint64  `json:"likely_memory_bytes"`
	MachineMemory uint64  `json:"machine_memory_bytes"`
	MemoryMeasure string  `json:"memory_measure"`
	CPUPercent    float64 `json:"cpu_percent"`
	Processes     int     `json:"processes"`
	MCPServers    int     `json:"mcp_servers"`
}
type SessionCost struct {
	MemoryMeasure string           `json:"memory_measure"`
	SessionID     int64            `json:"session_id"`
	Adapter       string           `json:"adapter"`
	Project       string           `json:"project"`
	StartedFrom   string           `json:"started_from,omitempty"`
	Memory        uint64           `json:"memory_bytes"`
	CPUPercent    float64          `json:"cpu_percent"`
	Processes     []MachineProcess `json:"processes"`
}

func mergeMeasure(current, next string) string {
	if current == "" {
		return next
	}
	if current != next {
		return "mixed"
	}
	return current
}
func placedProcess(p model.Proc, placement derive.ProcPlacement) MachineProcess {
	p.SessionID = 0
	row := MachineProcess{Proc: p, Memory: placement.Memory}
	row.MemoryMeasure = placement.Measure
	if a := placement.Attribution; a != nil {
		row.SessionID = a.SessionID
		row.Attribution = &MachineAttribution{SessionID: a.SessionID, Project: a.Project, Proof: a.Proof}
	}
	return row
}
func sortMachineProcesses(ps []MachineProcess) {
	sort.Slice(ps, func(i, j int) bool {
		if ps[i].Memory != ps[j].Memory {
			return ps[i].Memory > ps[j].Memory
		}
		return ps[i].PID < ps[j].PID
	})
}
func sortedApps(groups map[string]*MachineApp) []MachineApp {
	apps := make([]MachineApp, 0, len(groups))
	for _, a := range groups {
		sortMachineProcesses(a.Processes)
		apps = append(apps, *a)
	}
	sort.Slice(apps, func(i, j int) bool {
		if apps[i].Memory != apps[j].Memory {
			return apps[i].Memory > apps[j].Memory
		}
		return apps[i].ID < apps[j].ID
	})
	return apps
}
func buildAgentShare(v *MachineView, procs []model.Proc, sessions []model.Session, placements map[int32]derive.ProcPlacement) {
	v.AgentShare = AgentShare{MachineMemory: v.Sample.MemUsed}
	v.SessionCosts = []SessionCost{}
	v.OtherApps = MachineApp{ID: "other", Name: "Other apps", Processes: []MachineProcess{}}
	bySession := map[int64]model.Session{}
	for _, s := range sessions {
		bySession[s.ID] = s
	}
	byPID := map[int32]model.Proc{}
	for _, p := range procs {
		byPID[p.PID] = p
	}
	costs := map[int64]*SessionCost{}
	agents, likely := map[string]*MachineApp{}, map[string]*MachineApp{}
	sigs := signatures.Builtin()
	for _, p := range procs {
		placement, ok := placements[p.PID]
		if !ok || p.Zombie {
			continue
		}
		row := placedProcess(p, placement)
		if placement.Bucket == derive.BucketSession || placement.Bucket == derive.BucketAgent {
			v.AgentShare.Memory += row.Memory
			v.AgentShare.CPUPercent += p.CPUPercent
			v.AgentShare.Processes++
			v.AgentShare.MemoryMeasure = mergeMeasure(v.AgentShare.MemoryMeasure, placement.Measure)
		}
		switch placement.Bucket {
		case derive.BucketSession:
			a := placement.Attribution
			c := costs[a.SessionID]
			if c == nil {
				s := bySession[a.SessionID]
				c = &SessionCost{SessionID: a.SessionID, Adapter: s.Adapter, Project: a.Project, StartedFrom: s.StartedFrom, Processes: []MachineProcess{}}
				// Launch surface follows the session root, never a helper's own bundle.
				if root, ok := placements[s.PID]; ok && c.StartedFrom == "" {
					c.StartedFrom = strings.TrimSuffix(path.Base(root.Bundle), ".app")
					if root.Bundle == "" {
						c.StartedFrom = ""
					}
				}
				costs[a.SessionID] = c
			}
			c.Memory += row.Memory
			c.MemoryMeasure = mergeMeasure(c.MemoryMeasure, row.MemoryMeasure)
			c.CPUPercent += p.CPUPercent
			c.Processes = append(c.Processes, row)
		case derive.BucketAgent, derive.BucketLikely:
			name := sigs.ProcessName(p.Cmdline, p.Exe)
			id := "exe:" + name
			groups := agents
			if placement.Bucket == derive.BucketLikely {
				groups = likely
				v.AgentShare.LikelyMemory += row.Memory
				bundle := placement.Bundle
				if host, ok := placements[placement.SharedHost]; ok && host.Bundle != "" {
					bundle = host.Bundle
				}
				name = strings.TrimSuffix(path.Base(bundle), ".app")
				id = bundle
				if bundle == "" {
					host := byPID[placement.SharedHost]
					name = sigs.ProcessName(host.Cmdline, host.Exe)
					id = "exe:" + name
				}
				name += " (shared with editor)"
			}
			app := groups[id]
			if app == nil {
				app = &MachineApp{ID: id, Name: name, Processes: []MachineProcess{}}
				groups[id] = app
			}
			app.Memory += row.Memory
			app.CPUPercent += p.CPUPercent
			app.MemoryMeasure = mergeMeasure(app.MemoryMeasure, placement.Measure)
			app.Processes = append(app.Processes, row)
		default:
			v.OtherApps.Memory += row.Memory
			v.OtherApps.CPUPercent += p.CPUPercent
			v.OtherApps.MemoryMeasure = mergeMeasure(v.OtherApps.MemoryMeasure, placement.Measure)
		}
	}
	for _, c := range costs {
		sortMachineProcesses(c.Processes)
		v.SessionCosts = append(v.SessionCosts, *c)
	}
	sort.Slice(v.SessionCosts, func(i, j int) bool {
		if v.SessionCosts[i].Memory != v.SessionCosts[j].Memory {
			return v.SessionCosts[i].Memory > v.SessionCosts[j].Memory
		}
		return v.SessionCosts[i].SessionID < v.SessionCosts[j].SessionID
	})
	v.AgentNoSession = sortedApps(agents)
	v.LikelyAgent = sortedApps(likely)
	for _, c := range v.MCPServers.Commands {
		v.AgentShare.MCPServers += c.Count
	}
	if v.AgentShare.MemoryMeasure == "" {
		v.AgentShare.MemoryMeasure = "rss"
	}
}
