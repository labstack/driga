package panel

// HomeID is the dashboard the app opens on until the user saves their own.
const HomeID = "home"

// Home is the default dashboard. Its order follows what people supervising
// agents need most: what is waiting on them, what finished and needs review,
// runaway processes, agents that look stuck, work to resume, then disk.
func Home() Dashboard {
	scope := []Filter{{Column: "project", Value: "$project"}, {Column: "machine", Value: "$machine"}}
	with := func(f ...Filter) []Filter { return append(f, scope...) }
	return Dashboard{
		Name:        "Home",
		Description: "What needs you, what finished, what is running away, and what this machine can reclaim.",
		Variables:   []Variable{{Name: "project", Field: "project"}, {Name: "machine", Field: "machine"}},
		Panels: []Panel{
			// 1 and 2: what needs you and what finished, in one queue.
			{ID: "inbox", Title: "Inbox", Description: "Waits first, longest waiting first, then completions to review.", Viz: "inbox", Source: "inbox", Where: with(), Width: 8, Height: "l"},
			{ID: "waiting", Title: "Need you", Viz: "stat", Source: "inbox", Where: with(Filter{Column: "kind", Value: "waiting"}), Width: 4, Height: "s"},
			{ID: "finished", Title: "Finished, not acknowledged", Viz: "stat", Source: "inbox", Where: with(Filter{Column: "kind", Value: "finished"}), Width: 4, Height: "s"},
			{ID: "stuck_count", Title: "Possibly stuck", Viz: "stat", Source: "sessions", Where: with(Filter{Column: "label", Value: "stalled?"}), Width: 4, Height: "s"},
			// 4: runaway processes and memory.
			{ID: "orphans", Title: "Leftover process trees", Description: "Helper processes whose agent is gone. Only verified idle trees can be stopped.", Viz: "list", Source: "orphans", Columns: []string{"signature", "processes", "rss_bytes", "verified"}, Sort: "-rss_bytes", Where: []Filter{{Column: "machine", Value: "$machine"}}, Width: 6, Height: "m"},
			{ID: "costs", Title: "Memory by session", Viz: "bar", Source: "costs", By: "project", Measure: "sum:mem_bytes", Width: 6, Height: "m"},
			// 5: agents that look stuck.
			{ID: "stuck", Title: "Quiet longer than expected", Description: "Busy sessions with no new activity past stalled_after; suspected, not proven.", Viz: "table", Source: "sessions", Columns: []string{"id", "title", "agent", "project", "age", "last_activity_at"}, Sort: "last_activity_at", Where: with(Filter{Column: "label", Value: "stalled?"}), Width: 12, Height: "s"},
			// 6: work in flight, to find and resume.
			{ID: "running", Title: "Sessions", Viz: "table", Source: "sessions", Columns: []string{"id", "title", "label", "agent", "project", "branch", "age", "mem_bytes"}, Sort: "-last_activity_at", Where: with(), Width: 12, Height: "m"},
			// 7: disk agents left behind, and the machine's recent load.
			{ID: "reclaim", Title: "Reclaimable", Viz: "stat", Source: "summary", Measure: "sum:reclaim_bytes", Width: 4, Height: "s"},
			{ID: "machine_memory", Title: "Machine memory used", Viz: "timeseries", Source: "activity", Measure: "max:mem_used_bytes", Where: []Filter{{Column: "machine", Value: "$machine"}}, Width: 8, Height: "m"},
		},
	}
}
