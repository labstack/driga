package panel

// HomeID is the dashboard the app opens on until the user saves their own.
const HomeID = "home"

// BuiltinIDs are the dashboards Driga ships, in the order the app lists
// them. Each is a starting point the user can edit like any other; until
// they save one, its spec comes from here.
var BuiltinIDs = []string{HomeID, "flow", "projects", "machines"}

// Builtin returns a shipped dashboard by id.
func Builtin(id string) (Dashboard, bool) {
	switch id {
	case HomeID:
		return Home(), true
	case "flow":
		return Flow(), true
	case "projects":
		return Projects(), true
	case "machines":
		return Machines(), true
	}
	return Dashboard{}, false
}

var (
	scopeVars = []Variable{{Name: "project", Field: "project"}, {Name: "machine", Field: "machine"}}
	scope     = []Filter{{Column: "project", Value: "$project"}, {Column: "machine", Value: "$machine"}}
	byMachine = []Filter{{Column: "machine", Value: "$machine"}}
)

func with(f ...Filter) []Filter { return append(f, scope...) }

// Home is the default dashboard. Its order follows what people supervising
// agents need most: what is waiting on them, what finished and needs review,
// runaway processes, agents that look stuck, work to resume, then disk.
func Home() Dashboard {
	return Dashboard{
		Name:        "Home",
		Description: "What needs you, what finished, what is running away, and what this machine can reclaim.",
		Variables:   scopeVars,
		Panels: []Panel{
			// 1 and 2: what needs you and what finished, in one queue.
			{ID: "inbox", Title: "Inbox", Description: "Waits first, longest waiting first, then completions to review.", Viz: "inbox", Source: "inbox", Where: with(), Width: 8, Height: "l"},
			{ID: "waiting", Title: "Need you", Viz: "stat", Source: "inbox", Where: with(Filter{Column: "kind", Value: "waiting"}), Width: 4, Height: "s"},
			{ID: "finished", Title: "Finished, not acknowledged", Viz: "stat", Source: "inbox", Where: with(Filter{Column: "kind", Value: "finished"}), Width: 4, Height: "s"},
			{ID: "stuck_count", Title: "Possibly stuck", Viz: "stat", Source: "sessions", Where: with(Filter{Column: "label", Value: "stalled?"}), Width: 4, Height: "s"},
			// 4: runaway processes and memory.
			{ID: "orphans", Title: "Leftover process trees", Description: "Helper processes whose agent is gone. Only verified idle trees can be stopped.", Viz: "list", Source: "orphans", Columns: []string{"signature", "processes", "rss_bytes", "verified"}, Sort: "-rss_bytes", Where: byMachine, Width: 6, Height: "m"},
			{ID: "costs", Title: "Memory by project", Viz: "bar", Source: "costs", By: "project", Measure: "sum:mem_bytes", Width: 6, Height: "m"},
			// 5: agents that look stuck.
			{ID: "stuck", Title: "Quiet longer than expected", Description: "Busy sessions with no new activity past stalled_after; suspected, not proven.", Viz: "table", Source: "sessions", Columns: []string{"id", "title", "agent", "project", "age", "last_activity_at"}, Sort: "last_activity_at", Where: with(Filter{Column: "label", Value: "stalled?"}), Width: 12, Height: "s"},
			// 6: work in flight, to find and resume.
			{ID: "running", Title: "Sessions", Viz: "table", Source: "sessions", Columns: []string{"id", "title", "label", "agent", "project", "branch", "age", "mem_bytes"}, Sort: "-last_activity_at", Where: with(), Width: 12, Height: "m"},
			// 7: disk agents left behind, and the machine's recent load.
			{ID: "reclaim", Title: "Reclaimable", Viz: "stat", Source: "summary", Measure: "sum:reclaim_bytes", Width: 4, Height: "s"},
			{ID: "machine_memory", Title: "Machine memory used", Viz: "timeseries", Source: "activity", Measure: "max:mem_used_bytes", Where: byMachine, Width: 8, Height: "m"},
		},
	}
}

// Flow shows sessions over time: who ran when, where they waited on a
// person, and what the window cost.
func Flow() Dashboard {
	return Dashboard{
		Name:        "Flow",
		Description: "Every session in the window: when it ran, where it waited on you, and what it cost.",
		Variables:   append(scopeVars, Variable{Name: "agent", Field: "agent"}),
		Panels: []Panel{
			{ID: "timeline", Title: "Timeline", Description: "Each session from start to end; waits on you are marked.", Viz: "timeline", Source: "flow", Where: with(Filter{Column: "agent", Value: "$agent"}), Limit: 40, Width: 12, Height: "l"},
			{ID: "waiting", Title: "Waiting now", Viz: "stat", Source: "flow", Where: with(Filter{Column: "label", Value: "waiting"}, Filter{Column: "agent", Value: "$agent"}), Width: 3, Height: "s"},
			{ID: "working", Title: "Working", Viz: "stat", Source: "flow", Where: with(Filter{Column: "label", Value: "working"}, Filter{Column: "agent", Value: "$agent"}), Width: 3, Height: "s"},
			{ID: "cost", Title: "Cost in window", Description: "From retained API telemetry; sessions without it are not counted.", Viz: "stat", Source: "flow", Measure: "sum:cost_usd", Where: with(Filter{Column: "agent", Value: "$agent"}), Width: 3, Height: "s"},
			{ID: "output", Title: "Output tokens", Viz: "stat", Source: "flow", Measure: "sum:output_tokens", Where: with(Filter{Column: "agent", Value: "$agent"}), Width: 3, Height: "s"},
			{ID: "by_agent", Title: "Sessions by agent", Viz: "bar", Source: "flow", By: "agent", Where: with(), Width: 6, Height: "m"},
			{ID: "waits", Title: "Waits in window", Viz: "table", Source: "waits", Columns: []string{"title", "project", "reason", "started_at", "ended_at"}, Sort: "-started_at", Where: []Filter{{Column: "project", Value: "$project"}, {Column: "agent", Value: "$agent"}}, Width: 6, Height: "m"},
			{ID: "sessions", Title: "Sessions", Viz: "table", Source: "flow", Columns: []string{"id", "title", "label", "agent", "model", "project", "elapsed", "cost_usd", "last_tool"}, Sort: "-last_activity_at", Where: with(Filter{Column: "agent", Value: "$agent"}), Width: 12, Height: "m"},
		},
	}
}

// Projects shows each repository's live agents and unfinished work.
func Projects() Dashboard {
	return Dashboard{
		Name:        "Projects",
		Description: "Each repository with its live agents, uncommitted and unpushed work, and risks.",
		Variables:   []Variable{{Name: "machine", Field: "machine"}},
		Panels: []Panel{
			{ID: "cards", Title: "Projects", Viz: "cards", Source: "projects", Columns: []string{"project", "branch", "live_agents", "dirty", "unpushed", "worktrees", "at_risk"}, Sort: "-live_agents", Where: byMachine, Width: 12, Height: "m"},
			{ID: "agents", Title: "Live agents by project", Viz: "bar", Source: "sessions", By: "project", Where: byMachine, Width: 6, Height: "m"},
			{ID: "unpushed", Title: "Unpushed commits by project", Viz: "bar", Source: "projects", By: "project", Measure: "sum:unpushed", Where: byMachine, Width: 6, Height: "m"},
		},
	}
}

// Machines shows health and reclamation: memory and load over time, what
// agents cost, and what can be reclaimed.
func Machines() Dashboard {
	return Dashboard{
		Name:        "Machines",
		Description: "Machine health, what agents cost in memory and CPU, and what can be reclaimed.",
		Variables:   []Variable{{Name: "machine", Field: "machine"}},
		Panels: []Panel{
			{ID: "machines", Title: "Machines", Viz: "cards", Source: "machines", Columns: []string{"machine", "state", "os", "agents", "inbox", "last_seen"}, Width: 12, Height: "m"},
			{ID: "memory", Title: "Memory used", Viz: "timeseries", Source: "activity", By: "machine", Measure: "max:mem_used_bytes", Where: byMachine, Width: 6, Height: "m"},
			{ID: "load", Title: "Load average", Viz: "timeseries", Source: "activity", By: "machine", Measure: "max:load1", Where: byMachine, Width: 6, Height: "m"},
			{ID: "agent_memory", Title: "Agent memory by role", Viz: "bar", Source: "memory", By: "role", Measure: "sum:rss_bytes", Where: byMachine, Width: 6, Height: "m"},
			{ID: "costs", Title: "Sessions by cost", Viz: "table", Source: "costs", Columns: []string{"session_id", "agent", "project", "mem_bytes", "cpu_percent", "processes"}, Sort: "-mem_bytes", Width: 6, Height: "m"},
			{ID: "orphans", Title: "Leftover process trees", Description: "Only verified idle trees can be stopped.", Viz: "table", Source: "orphans", Columns: []string{"signature", "root_pid", "processes", "rss_bytes", "age", "verified"}, Sort: "-rss_bytes", Where: byMachine, Width: 6, Height: "m"},
			{ID: "cleanup", Title: "Disk agents left behind", Description: "Removal checks each candidate again first.", Viz: "table", Source: "cleanup", Columns: []string{"kind", "path", "status", "size_bytes"}, Sort: "-size_bytes", Width: 6, Height: "m"},
			{ID: "disk", Title: "Free disk", Viz: "timeseries", Source: "activity", By: "machine", Measure: "min:disk_free_bytes", Where: byMachine, Width: 12, Height: "s"},
		},
	}
}
