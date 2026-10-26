package panel

import "fmt"

// HeaderID is the app's header: the few numbers that say whether anything
// needs the user, shown on every view. HomeID is the dashboard the app opens
// on, with Chat beside it. People change both like any dashboard, by asking
// Chat or in the app; every other dashboard is one they or Chat made.
const (
	HeaderID = "header"
	HomeID   = "home"
)

// BuiltinIDs are the dashboards Driga ships. Until the user saves one, its
// spec comes from here; deleting it brings the shipped one back.
var BuiltinIDs = []string{HeaderID, HomeID}

// Builtin returns a shipped dashboard by id.
func Builtin(id string) (Dashboard, bool) {
	switch id {
	case HeaderID:
		return Header(), true
	case HomeID:
		return Home(), true
	}
	return Dashboard{}, false
}

// Home follows what people supervising agents need most, after the counts
// the header already shows: what waits on them and what finished, runaway
// processes, agents that look stuck, work in flight, then memory.
func Home() Dashboard {
	scope := []Filter{{Column: "project", Value: "$project"}, {Column: "machine", Value: "$machine"}}
	with := func(f ...Filter) []Filter { return append(f, scope...) }
	return Dashboard{
		Name:        "Home",
		Description: "What needs you, what finished, what is running away, and what is in flight.",
		Variables:   []Variable{{Name: "project", Field: "project"}, {Name: "machine", Field: "machine"}},
		Panels: []Panel{
			{ID: "inbox", Title: "Inbox", Description: "Waits first, longest waiting first, then each session's latest finished turn to review.", Viz: "inbox", Source: "inbox", Where: with(), Width: 7, Height: "m",
				Empty: "Nothing needs you. Agents waiting on you, and sessions that finished, show here."},
			{ID: "orphans", Title: "Leftover process trees", Description: "Helper processes whose agent is gone. Only verified idle trees can be stopped.", Viz: "list", Source: "orphans", Columns: []string{"signature", "processes", "rss_bytes", "verified"}, Sort: "-rss_bytes", Where: []Filter{{Column: "machine", Value: "$machine"}}, Width: 5, Height: "m",
				Empty: "No leftover processes. Helper trees an agent left running after it ended show here."},
			{ID: "stuck", Title: "Quiet longer than expected", Description: "Busy sessions with no new activity past stuck_after; suspected, not proven.", Viz: "table", Source: "sessions", Columns: []string{"title", "agent", "project", "machine", "last_activity_at", "evidence"}, Sort: "last_activity_at", Where: with(Filter{Column: "state", Value: "stuck"}), Width: 12, Height: "s",
				Empty: "No session looks stuck. A busy session quiet past stuck_after shows here."},
			{ID: "sessions", Title: "Sessions", Viz: "table", Source: "sessions", Columns: []string{"state", "title", "project", "branch", "agent", "machine", "last_activity_at", "mem_bytes"}, Where: with(), Width: 12, Height: "m",
				Empty: "No live sessions. Agent sessions on your machines show here while they run."},
			{ID: "memory", Title: "Memory by project, this machine", Description: "Memory held by agents and the processes they started, on this machine only; other machines don't report it yet.", Viz: "bar", Source: "costs", By: "project", Measure: "sum:mem_bytes", Width: 6, Height: "m"},
			{ID: "machine_memory", Title: "Machine memory used", Description: "Each machine's used memory over the last hour, one line per machine.", Viz: "timeseries", Source: "activity", By: "machine", Measure: "max:mem_used_bytes", Where: []Filter{{Column: "machine", Value: "$machine"}}, Width: 6, Height: "m"},
		},
	}
}

// Header's metrics follow what people supervising agents need most: what
// waits on them, what finished for review, agents that look stuck, then
// what runaway processes and leftover disk cost, and the machine's memory
// over the last hour.
func Header() Dashboard {
	return Dashboard{
		Name:        "Header",
		Description: "The numbers shown above Chat in the app. Only stat panels.",
		Panels: []Panel{
			{ID: "waiting", Title: "Need you", Description: "Agents waiting on you and machine warnings in effect.", Viz: "stat", Source: "summary", Measure: "sum:need_you"},
			{ID: "finished", Title: "To review", Viz: "stat", Source: "summary", Measure: "sum:to_review"},
			{ID: "stuck", Title: "Stuck", Viz: "stat", Source: "summary", Measure: "sum:attention_stuck"},
			{ID: "leftover", Title: "Leftover memory", Viz: "stat", Source: "summary", Measure: "sum:leftover_bytes"},
			{ID: "reclaim", Title: "Reclaimable", Viz: "stat", Source: "summary", Measure: "sum:reclaim_bytes"},
			{ID: "memory", Title: "Memory used", Viz: "stat", Source: "local_activity", Measure: "max:mem_used_bytes", Total: "max:mem_total_bytes"},
		},
	}
}

// ValidateHeader checks what the header can show: a short row of stats.
func ValidateHeader(d Dashboard) error {
	var problems Problems
	if len(d.Panels) > 8 {
		problems = append(problems, Problem{Path: "panels", Message: "the header shows at most 8 metrics"})
	}
	for i, p := range d.Panels {
		if p.Viz != "stat" {
			problems = append(problems, Problem{Path: fmt.Sprintf("panels[%d].viz", i), Message: "the header shows only stat panels"})
		}
	}
	if len(problems) > 0 {
		return problems
	}
	return nil
}
