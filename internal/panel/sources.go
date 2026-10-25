package panel

import (
	"slices"
	"strings"
	"time"

	"github.com/labstack/driga/internal/view"
	"github.com/labstack/driga/internal/viewer"
)

// Column types a frame carries. Times travel as RFC 3339 strings; durations
// and bytes are numbers whose unit says how to read them.
const (
	String = "string"
	Number = "number"
	Time   = "time"
	Bool   = "bool"
)

// Column describes one frame column.
type Column struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Unit string `json:"unit,omitempty"`
}

// Frame is a source's rows, or a panel's result after filtering and
// aggregation.
type Frame struct {
	Columns []Column `json:"columns"`
	Rows    [][]any  `json:"rows"`
}

type schema struct {
	description string
	columns     []Column
	build       func(viewer.Snapshot) [][]any
}

func (s schema) column(name string) *Column {
	for i := range s.columns {
		if s.columns[i].Name == name {
			return &s.columns[i]
		}
	}
	return nil
}

func (s schema) names() string {
	names := make([]string, len(s.columns))
	for i, c := range s.columns {
		names[i] = c.Name
	}
	return strings.Join(names, ", ")
}

func col(name, typ string, unit ...string) Column {
	c := Column{Name: name, Type: typ}
	if len(unit) > 0 {
		c.Unit = unit[0]
	}
	return c
}

func stamp(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func ms(d time.Duration) float64 { return float64(d.Milliseconds()) }

// sources are the data a panel can show, each read from the same snapshot
// the app's views use.
var sources = map[string]schema{
	"sessions": {
		description: "Every live agent session, workers included, with its state label",
		columns:     []Column{col("id", Number), col("title", String), col("label", String), col("agent", String), col("project", String), col("branch", String), col("machine", String), col("age", Number, "duration"), col("mem_bytes", Number, "bytes"), col("last_activity_at", Time)},
		build: func(s viewer.Snapshot) [][]any {
			var rows [][]any
			var walk func([]view.AgentRow)
			walk = func(agents []view.AgentRow) {
				for _, a := range agents {
					rows = append(rows, []any{float64(a.ID), a.Title, a.Label, a.Adapter, a.Project, a.Branch, a.MachineName, ms(a.Age), float64(a.Mem), stamp(a.LastActivityAt)})
					walk(a.Children)
				}
			}
			walk(s.Agents.Rows)
			return rows
		},
	},
	"inbox": {
		description: "What needs you and what finished: waits, completions and machine warnings",
		columns:     []Column{col("id", String), col("kind", String), col("reason", String), col("tool", String), col("detail", String), col("summary", String), col("title", String), col("project", String), col("branch", String), col("agent", String), col("session_id", Number), col("machine_id", String), col("machine", String), col("waited", Number, "duration"), col("age", Number, "duration"), col("occurred_at", Time), col("ack_pending", Bool)},
		build: func(s viewer.Snapshot) [][]any {
			rows := make([][]any, 0, len(s.Inbox.Rows))
			for _, r := range s.Inbox.Rows {
				if r.Historical {
					continue
				}
				rows = append(rows, []any{r.ID, r.Kind, r.Reason, r.Tool, r.Detail, r.Summary, r.Title, r.Project, r.Branch, r.Adapter, float64(r.SessionID), r.MachineID, r.MachineName, ms(r.Waited), ms(r.Age), stamp(r.TS), r.AckPendingAt != nil})
			}
			return rows
		},
	},
	"projects": {
		description: "Repositories with their live agents, uncommitted and unpushed work",
		columns:     []Column{col("id", Number), col("project", String), col("path", String), col("branch", String), col("machine", String), col("live_agents", Number, "count"), col("dirty", Number, "count"), col("unpushed", Number, "count"), col("worktrees", Number, "count"), col("activity", String), col("at_risk", String)},
		build: func(s viewer.Snapshot) [][]any {
			rows := make([][]any, 0, len(s.Projects.Rows))
			for _, p := range s.Projects.Rows {
				rows = append(rows, []any{float64(p.Status.Repo.ID), p.Status.Repo.Name, p.Status.Repo.Path, p.Branch, p.MachineName, float64(p.Status.LiveAgents), float64(p.Dirty), float64(p.Unpushed), float64(p.Worktrees), p.Status.Activity, strings.Join(p.Status.AtRisk, ", ")})
			}
			return rows
		},
	},
	"orphans": {
		description: "Process trees left behind by agents that are gone",
		columns:     []Column{col("id", String), col("signature", String), col("root_pid", Number), col("machine_id", String), col("machine", String), col("processes", Number, "count"), col("rss_bytes", Number, "bytes"), col("age", Number, "duration"), col("verified", Bool), col("young", Bool), col("cwd", String)},
		build: func(s viewer.Snapshot) [][]any {
			rows := make([][]any, 0, len(s.Orphans.Rows))
			for _, o := range s.Orphans.Rows {
				rows = append(rows, []any{o.ID, o.Signature, float64(o.RootPID), o.MachineID, o.MachineName, float64(o.ProcCount), float64(o.RSSTotal), ms(o.Age), o.Verified, o.Young, o.Cwd})
			}
			return rows
		},
	},
	"cleanup": {
		description: "Disk agents left behind on this machine: merged worktrees and scratch directories",
		columns:     []Column{col("id", String), col("kind", String), col("path", String), col("project", String), col("branch", String), col("status", String), col("size_bytes", Number, "bytes")},
		build: func(s viewer.Snapshot) [][]any {
			rows := make([][]any, 0, len(s.Cleanup))
			for _, c := range s.Cleanup {
				rows = append(rows, []any{c.ID, string(c.Kind), c.Path, c.Repo, c.Branch, c.Status, float64(c.Size)})
			}
			return rows
		},
	},
	"activity": {
		description: "One row per machine per minute for the last hour: events, memory, load and disk",
		columns:     []Column{col("ts", Time), col("machine", String), col("events", Number, "count"), col("mem_used_bytes", Number, "bytes"), col("mem_total_bytes", Number, "bytes"), col("swap_used_bytes", Number, "bytes"), col("load1", Number), col("disk_free_bytes", Number, "bytes")},
		build: func(s viewer.Snapshot) [][]any {
			var rows [][]any
			for _, m := range s.Activity.Machines {
				for _, b := range m.Buckets {
					row := []any{stamp(b.TS), m.MachineName, float64(b.Events), nil, nil, nil, nil, nil}
					if b.Sample != nil {
						row[3], row[4], row[5], row[6], row[7] = float64(b.Sample.MemUsed), float64(b.Sample.MemTotal), float64(b.Sample.SwapUsed), b.Sample.Load1, float64(b.Sample.DiskFree)
					}
					rows = append(rows, row)
				}
			}
			return rows
		},
	},
	"memory": {
		description: "Agent process memory by agent and role",
		columns:     []Column{col("agent", String), col("role", String), col("machine", String), col("rss_bytes", Number, "bytes")},
		build: func(s viewer.Snapshot) [][]any {
			rows := make([][]any, 0, len(s.Machine.Memory))
			for _, m := range s.Machine.Memory {
				rows = append(rows, []any{m.Agent, m.Role, m.MachineName, float64(m.RSS)})
			}
			return rows
		},
	},
	"costs": {
		description: "Memory and CPU each live session costs on this machine",
		columns:     []Column{col("session_id", Number), col("agent", String), col("project", String), col("mem_bytes", Number, "bytes"), col("cpu_percent", Number, "percent"), col("processes", Number, "count")},
		build: func(s viewer.Snapshot) [][]any {
			rows := make([][]any, 0, len(s.Machine.SessionCosts))
			for _, c := range s.Machine.SessionCosts {
				rows = append(rows, []any{float64(c.SessionID), c.Adapter, c.Project, float64(c.Memory), c.CPUPercent, float64(len(c.Processes))})
			}
			return rows
		},
	},
	"machines": {
		description: "Known machines with their agents, inbox and connection state",
		columns:     []Column{col("machine_id", String), col("machine", String), col("state", String), col("os", String), col("agents", Number, "count"), col("inbox", Number, "count"), col("last_seen", Number, "duration")},
		build: func(s viewer.Snapshot) [][]any {
			rows := make([][]any, 0, len(s.Machines.Rows))
			for _, m := range s.Machines.Rows {
				rows = append(rows, []any{m.MachineID, view.MachineName(m.Machine), view.MachineState(m.Machine, s.LocalMachineID, s.ObservedAt), m.OS, float64(m.Agents), float64(m.Inbox), ms(m.LastSeenAge)})
			}
			return rows
		},
	},
	"summary": {
		description: "One row of totals for the selected machines",
		columns:     []Column{col("agents", Number, "count"), col("working", Number, "count"), col("waiting", Number, "count"), col("stalled", Number, "count"), col("inbox", Number, "count"), col("orphans", Number, "count"), col("reclaim_bytes", Number, "bytes"), col("mem_used_bytes", Number, "bytes"), col("mem_total_bytes", Number, "bytes"), col("disk_free_bytes", Number, "bytes"), col("load1", Number)},
		build: func(s viewer.Snapshot) [][]any {
			v := s.Summary
			return [][]any{{float64(v.Agents), float64(v.Working), float64(v.Waiting), float64(v.Stalled), float64(v.Inbox), float64(v.Orphans), float64(v.Reclaim), float64(v.Sample.MemUsed), float64(v.Sample.MemTotal), float64(v.Sample.DiskFree), v.Sample.Load1}}
		},
	},
}

// SourceNames lists the sources in a stable order.
func SourceNames() []string {
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// SourceInfo describes one source for discovery.
type SourceInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Columns     []Column `json:"columns"`
}

// Sources describes every source and its columns.
func Sources() []SourceInfo {
	out := make([]SourceInfo, 0, len(sources))
	for _, name := range SourceNames() {
		s := sources[name]
		out = append(out, SourceInfo{Name: name, Description: s.description, Columns: s.columns})
	}
	return out
}

// variableColumns maps a variable's field to the column it filters in each
// source that has one.
var variableColumns = map[string]string{"project": "project", "machine": "machine", "agent": "agent", "label": "label", "kind": "kind"}
