package panel

import (
	"cmp"
	"encoding/json"
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
	// Hidden columns carry a row's identity for actions, not for display.
	Hidden bool `json:"hidden,omitempty"`
}

// Frame is a source's rows, or a panel's result after filtering and
// aggregation.
type Frame struct {
	Columns []Column `json:"columns"`
	Rows    [][]any  `json:"rows"`
	// Spans are the waits of a timeline's sessions, by session id.
	Spans []Span `json:"spans,omitempty"`
}

// Span is one stretch of a session's timeline, such as a wait on a person.
type Span struct {
	MachineID string  `json:"machine_id"`
	SessionID float64 `json:"session_id"`
	Kind      string  `json:"kind"`
	From      any     `json:"from"`
	To        any     `json:"to"`
	Reason    string  `json:"reason,omitempty"`
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
	"activity_heatmap": {
		description: "Tool calls per project per UTC hour for the last 24 hours (including the current hour). Zero-filled. The largest transcript/hook/OTLP count per session/hour avoids summing duplicate reporting channels. No text or file contents.",
		columns:     []Column{col("ts", Time), col("project", String), col("machine_id", String), col("machine", String), col("tool_calls", Number, "count")},
		build: func(s viewer.Snapshot) [][]any {
			var rows [][]any
			for _, h := range s.ToolActivity {
				rows = append(rows, []any{stamp(h.TS), h.Project, h.MachineID, h.Machine, float64(h.Calls)})
			}
			return rows
		},
	},
	"sessions": {
		description: "Every live agent session, workers included, with last 30 completed turn outcomes (comma-separated ok, waited, failed), files count and edited (JSON array of project-relative captured paths), ordered by state (waiting, then stuck or dead, working, idle, finished, other) and newest first within each. label is Driga's raw label (a leading ~ marks an inferred one); state is the label without the mark, inferred says whether it was inferred, and evidence is why",
		columns:     []Column{col("id", Number), col("title", String), col("label", String), col("agent", String), col("project", String), col("branch", String), col("machine", String), col("age", Number, "duration"), col("mem_bytes", Number, "bytes"), col("last_activity_at", Time), col("machine_id", String), col("state", String), col("inferred", Bool), col("evidence", String), col("turns", String), col("files", Number, "count"), col("edited", String)},
		build: func(s viewer.Snapshot) [][]any {
			var rows [][]any
			var walk func([]view.AgentRow)
			walk = func(agents []view.AgentRow) {
				for _, a := range agents {
					rows = append(rows, []any{float64(a.ID), sessionTitle(a.Title, a.Adapter, a.Project, a.Branch), a.Label, a.Adapter, a.Project, a.Branch, a.MachineName, ms(a.Age), bytesKnown(a.Mem), stamp(a.LastActivityAt), a.MachineID, strings.TrimPrefix(a.Label, "~"), a.Heuristic || strings.HasPrefix(a.Label, "~"), a.Evidence, turnsFor(s, a.MachineID, a.ID), filesCount(s, a.MachineID, a.ID), filesFor(s, a.MachineID, a.ID)})
					walk(a.Children)
				}
			}
			walk(s.Agents.Rows)
			// What needs the person comes first, then what may be stuck, what
			// works, and what rests; newest first within each.
			slices.SortStableFunc(rows, func(a, b []any) int {
				return cmp.Or(cmp.Compare(urgency(a[11].(string)), urgency(b[11].(string))), cmp.Compare(a[7].(float64), b[7].(float64)))
			})
			return rows
		},
	},
	"inbox": {
		description: "What needs you and what finished: waits, completions and machine warnings. A session's finished turns are one item, its latest, with how many turns it holds. needs_you is true for a wait and for a machine warning still in effect, such as memory_pressure or low_disk. A finished item carries what there is to review: files the session edited, and its checkout's uncommitted, unpushed and latest commit",
		columns:     []Column{col("id", String), col("kind", String), col("reason", String), col("tool", String), col("detail", String), col("summary", String), col("title", String), col("project", String), col("branch", String), col("agent", String), col("session_id", Number), col("machine_id", String), col("machine", String), col("waited", Number, "duration"), col("age", Number, "duration"), col("occurred_at", Time), col("ack_pending", Bool), col("turns", Number, "count"), col("ids", String), col("files", Number, "count"), col("edited", String), col("checkout", String), col("dirty", Number, "count"), col("unpushed", Number, "count"), col("last_commit", String), col("needs_you", Bool)},
		build: func(s viewer.Snapshot) [][]any {
			type key struct {
				machine string
				session int64
			}
			rows := make([][]any, 0, len(s.Inbox.Rows))
			finished := map[key]int{}
			for _, r := range s.Inbox.Rows {
				if r.Historical {
					continue
				}
				row := []any{r.ID, r.Kind, r.Reason, r.Tool, r.Detail, r.Summary, sessionTitle(r.Title, r.Adapter, r.Project, r.Branch), r.Project, r.Branch, r.Adapter, float64(r.SessionID), r.MachineID, r.MachineName, ms(r.Waited), ms(r.Age), stamp(r.TS), r.AckPendingAt != nil, 1.0, r.ID, nil, nil, nil, nil, nil, nil, r.Kind == "waiting" || (r.Kind != "finished" && r.EndedAt == nil)}
				if r.Kind == "finished" {
					row[19], row[20] = float64(len(r.Files)), strings.Join(r.Files, "\n")
					if c := r.Checkout; c != nil {
						row[21], row[22], row[23], row[24] = c.Path, float64(c.Dirty+c.Untracked), float64(c.Ahead), c.LastCommitSubject
					}
				}
				k := key{r.MachineID, r.SessionID}
				if r.Kind != "finished" || r.SessionID == 0 {
					rows = append(rows, row)
					continue
				}
				// Review needs a session's latest finish, not every turn's.
				i, seen := finished[k]
				if !seen {
					finished[k] = len(rows)
					rows = append(rows, row)
					continue
				}
				kept := rows[i]
				turns, ids := kept[17].(float64)+1, kept[18].(string)+","+r.ID
				if latest, _ := kept[15].(string); stampAfter(r.TS, latest) {
					kept = row
				}
				kept[17], kept[18] = turns, ids
				rows[i] = kept
			}
			return rows
		},
	},
	"projects": {
		description: "Repositories with their live agents, uncommitted and unpushed work",
		columns:     []Column{col("id", Number), col("project", String), col("path", String), col("branch", String), col("machine", String), col("live_agents", Number, "count"), col("dirty", Number, "count"), col("unpushed", Number, "count"), col("worktrees", Number, "count"), col("activity", String), col("at_risk", String), col("machine_id", String)},
		build: func(s viewer.Snapshot) [][]any {
			rows := make([][]any, 0, len(s.Projects.Rows))
			for _, p := range s.Projects.Rows {
				rows = append(rows, []any{float64(p.Status.Repo.ID), p.Status.Repo.Name, p.Status.Repo.Path, p.Branch, p.MachineName, float64(p.Status.LiveAgents), float64(p.Dirty), float64(p.Unpushed), float64(p.Worktrees), p.Status.Activity, strings.Join(p.Status.AtRisk, ", "), p.MachineID})
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
		columns:     []Column{col("id", String), col("kind", String), col("path", String), col("project", String), col("branch", String), col("status", String), col("size_bytes", Number, "bytes"), col("machine_id", String)},
		build: func(s viewer.Snapshot) [][]any {
			rows := make([][]any, 0, len(s.Cleanup))
			for _, c := range s.Cleanup {
				rows = append(rows, []any{c.ID, string(c.Kind), c.Path, c.Repo, c.Branch, c.Status, float64(c.Size), s.LocalMachineID})
			}
			return rows
		},
	},
	"activity": {
		description: "One row per machine per minute for the last hour: events, memory, load and disk",
		columns:     []Column{col("ts", Time), col("machine", String), col("events", Number, "count"), col("mem_used_bytes", Number, "bytes"), col("mem_total_bytes", Number, "bytes"), col("swap_used_bytes", Number, "bytes"), col("load1", Number), col("disk_free_bytes", Number, "bytes"), col("disk_total_bytes", Number, "bytes"), col("cpu_percent", Number, "percent")},
		build: func(s viewer.Snapshot) [][]any {
			var rows [][]any
			for _, m := range s.Activity.Machines {
				for _, b := range m.Buckets {
					row := []any{stamp(b.TS), m.MachineName, float64(b.Events), nil, nil, nil, nil, nil, nil, nil}
					if b.Sample != nil {
						row[3], row[4], row[5], row[6], row[7] = float64(b.Sample.MemUsed), float64(b.Sample.MemTotal), float64(b.Sample.SwapUsed), b.Sample.Load1, float64(b.Sample.DiskFree)
						row[8], row[9] = float64(b.Sample.DiskTotal), cpuValue(b.Sample.CPUPercent)
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
		columns:     []Column{col("session_id", Number), col("agent", String), col("project", String), col("mem_bytes", Number, "bytes"), col("cpu_percent", Number, "percent"), col("processes", Number, "count"), col("title", String)},
		build: func(s viewer.Snapshot) [][]any {
			titles := map[sessionKey]string{}
			var walk func([]view.AgentRow)
			walk = func(agents []view.AgentRow) {
				for _, a := range agents {
					titles[sessionKey{a.MachineID, a.ID}] = sessionTitle(a.Title, a.Adapter, a.Project, a.Branch)
					walk(a.Children)
				}
			}
			walk(s.Agents.Rows)
			rows := make([][]any, 0, len(s.Machine.SessionCosts))
			for _, c := range s.Machine.SessionCosts {
				rows = append(rows, []any{float64(c.SessionID), c.Adapter, c.Project, float64(c.Memory), c.CPUPercent, float64(len(c.Processes)), cmp.Or(titles[sessionKey{s.LocalMachineID, c.SessionID}], sessionTitle("", c.Adapter, c.Project, ""))})
			}
			return rows
		},
	},
	"flow": {
		description: "Every session in the flow window, ended ones included, with its family, cost and model",
		columns:     []Column{col("id", Number), col("parent_id", Number), col("title", String), col("label", String), col("agent", String), col("model", String), col("project", String), col("branch", String), col("machine_id", String), col("machine", String), col("started_at", Time), col("ended_at", Time), col("last_activity_at", Time), col("elapsed", Number, "duration"), col("mem_bytes", Number, "bytes"), col("cost_usd", Number, "usd"), col("input_tokens", Number, "count"), col("output_tokens", Number, "count"), col("last_tool", String), col("reason", String)},
		build: func(s viewer.Snapshot) [][]any {
			names := machineNames(s)
			rows := make([][]any, 0, len(s.BoardData.Nodes))
			for _, n := range s.BoardData.Nodes {
				var parent, ended, mem, cost, in, out any
				if n.ParentID != nil {
					parent = float64(*n.ParentID)
				}
				if n.EndedAt != nil {
					ended = stamp(*n.EndedAt)
				}
				if n.Mem != nil {
					mem = float64(*n.Mem)
				}
				if n.CostUSD != nil {
					cost = *n.CostUSD
				}
				if n.InputTokens != nil {
					in = float64(*n.InputTokens)
				}
				if n.OutputTokens != nil {
					out = float64(*n.OutputTokens)
				}
				rows = append(rows, []any{float64(n.ID), parent, n.Title, n.Label, n.Adapter, n.Model, n.Project, n.Branch, n.MachineID, names[n.MachineID], stamp(n.StartedAt), ended, stamp(n.LastActivityAt), ms(n.Elapsed), mem, cost, in, out, n.LastTool, n.Reason})
			}
			return rows
		},
	},
	"history": {
		description: "Top-level sessions that ended in the last two weeks, newest first, with the command that resumes each where its agent can",
		columns:     []Column{col("id", Number), col("title", String), col("project", String), col("branch", String), col("agent", String), col("machine", String), col("machine_id", String), col("started_at", Time), col("ended_at", Time), col("resume", String)},
		build: func(s viewer.Snapshot) [][]any {
			names := machineNames(s)
			rows := make([][]any, 0, len(s.BoardData.History))
			for _, h := range s.BoardData.History {
				rows = append(rows, []any{float64(h.ID), h.Title, h.Project, h.Branch, h.Adapter, names[h.MachineID], h.MachineID, stamp(h.StartedAt), stamp(h.EndedAt), h.Resume})
			}
			return rows
		},
	},
	"waits": {
		description: "Spans when a session waited on a person, from the flow window",
		columns:     []Column{col("session_id", Number), col("title", String), col("project", String), col("agent", String), col("reason", String), col("detail", String), col("started_at", Time), col("ended_at", Time), col("machine", String), col("machine_id", String)},
		build: func(s viewer.Snapshot) [][]any {
			names := machineNames(s)
			var rows [][]any
			for _, n := range s.BoardData.Nodes {
				for _, w := range n.Waits {
					var ended any
					if w.EndedAt != nil {
						ended = stamp(*w.EndedAt)
					}
					rows = append(rows, []any{float64(n.ID), n.Title, n.Project, n.Adapter, w.Reason, w.Detail, stamp(w.StartedAt), ended, names[n.MachineID], n.MachineID})
				}
			}
			return rows
		},
	},
	"machines": {
		description: "Known machines with their agents, inbox and connection state",
		columns:     []Column{col("machine_id", String), col("machine", String), col("state", String), col("os", String), col("agents", Number, "count"), col("inbox", Number, "count"), col("last_seen", Number, "duration"), col("mem_used_bytes", Number, "bytes"), col("mem_total_bytes", Number, "bytes"), col("disk_free_bytes", Number, "bytes"), col("disk_total_bytes", Number, "bytes"), col("cpu_percent", Number, "percent")},
		build: func(s viewer.Snapshot) [][]any {
			rows := make([][]any, 0, len(s.Machines.Rows))
			for _, m := range s.Machines.Rows {
				row := []any{m.MachineID, view.MachineName(m.Machine), view.MachineState(m.Machine, s.LocalMachineID, s.ObservedAt), m.OS, float64(m.Agents), float64(m.Inbox), ms(m.LastSeenAge), nil, nil, nil, nil, nil}
				if m.Sample != nil {
					row[7], row[8], row[9], row[10], row[11] = float64(m.Sample.MemUsed), float64(m.Sample.MemTotal), float64(m.Sample.DiskFree), bytesKnown(m.Sample.DiskTotal), cpuValue(m.Sample.CPUPercent)
				}
				rows = append(rows, row)
			}
			return rows
		},
	},
	"summary": {
		description: "One row of totals for the selected machines",
		columns:     []Column{col("agents", Number, "count"), col("working", Number, "count"), col("waiting", Number, "count"), col("stuck", Number, "count"), col("inbox", Number, "count"), col("orphans", Number, "count"), col("reclaim_bytes", Number, "bytes"), col("mem_used_bytes", Number, "bytes"), col("mem_total_bytes", Number, "bytes"), col("disk_free_bytes", Number, "bytes"), col("load1", Number), col("disk_total_bytes", Number, "bytes"), col("cpu_percent", Number, "percent"), col("need_you", Number, "count"), col("to_review", Number, "count"), col("attention_stuck", Number, "count"), col("leftover", Number, "count"), col("leftover_bytes", Number, "bytes")},
		build: func(s viewer.Snapshot) [][]any {
			h := view.ProjectAttention(s.Inbox, s.Agents, s.LocalMachineID, s.LivePrompts).Header(s.Orphans, s.Summary)
			v := s.Summary
			return [][]any{{float64(v.Agents), float64(v.Working), float64(v.Waiting), float64(v.Stuck), float64(v.Inbox), float64(v.Orphans), float64(v.Reclaim), float64(v.Sample.MemUsed), float64(v.Sample.MemTotal), float64(v.Sample.DiskFree), v.Sample.Load1, float64(v.Sample.DiskTotal), cpuValue(v.Sample.CPUPercent), float64(h.NeedYou), float64(h.ToReview), float64(h.Stuck), float64(h.Leftover), float64(h.LeftoverBytes)}}
		},
	},
}

func init() {
	activity := sources["activity"]
	sources["local_activity"] = schema{description: "Health history for the summary machine only", columns: activity.columns, build: func(s viewer.Snapshot) [][]any {
		local := s
		local.Activity.Machines = nil
		for _, machine := range s.Activity.Machines {
			if machine.MachineID == s.Summary.MachineID {
				local.Activity.Machines = append(local.Activity.Machines, machine)
			}
		}
		return activity.build(local)
	}}
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

// machineNames maps machine ids to the names the other sources show.
func machineNames(s viewer.Snapshot) map[string]string {
	names := map[string]string{}
	for _, m := range s.Machines.Rows {
		names[m.MachineID] = view.MachineName(m.Machine)
	}
	return names
}

// urgency orders session states by how soon the person should look.
func urgency(state string) int {
	switch state {
	case "waiting":
		return 0
	case "stuck", "dead":
		return 1
	case "working":
		return 2
	case "idle":
		return 3
	case "finished":
		return 4
	}
	return 5
}

// sessionTitle names a session that has no title of its own by its agent,
// project and, off the main line, its branch, which is what tells two such
// sessions in one project apart.
func sessionTitle(title, agent, project, branch string) string {
	if title != "" || agent == "" {
		return title
	}
	name := agent + " session"
	if project != "" {
		name += " in " + project
	}
	switch branch {
	case "", "main", "master", "HEAD":
	default:
		name += " on " + branch
	}
	return name
}

// stampAfter reports whether t is later than a stamp, or the stamp is unset.
func stampAfter(t time.Time, stamp string) bool {
	at, err := time.Parse(time.RFC3339, stamp)
	return err != nil || t.After(at)
}

// bytesKnown is a memory reading, or none when the session's processes were
// not measured: zero bytes is never a real reading for a live session.
func bytesKnown(n uint64) any {
	if n == 0 {
		return nil
	}
	return float64(n)
}

func cpuValue(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func turnsFor(s viewer.Snapshot, machine string, id int64) string {
	for _, d := range s.SessionPanels {
		if d.ID == id && d.MachineID == machine {
			return strings.Join(d.Turns, ",")
		}
	}
	return ""
}
func filesCount(s viewer.Snapshot, machine string, id int64) float64 {
	for _, d := range s.SessionPanels {
		if d.ID == id && d.MachineID == machine {
			return float64(len(d.Files))
		}
	}
	return 0
}
func filesFor(s viewer.Snapshot, machine string, id int64) string {
	for _, d := range s.SessionPanels {
		if d.ID == id && d.MachineID == machine {
			if len(d.Files) == 0 {
				return "[]"
			}
			b, _ := json.Marshal(d.Files)
			return string(b)
		}
	}
	return "[]"
}

type sessionKey struct {
	machine string
	id      int64
}
