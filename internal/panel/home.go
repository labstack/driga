package panel

import "fmt"

// HeaderID is the app's header: the few numbers that say whether anything
// needs the user, shown above Chat on every view. People change it like any
// dashboard, by asking Chat or in the app. Every other dashboard is one Chat
// made.
const HeaderID = "header"

// BuiltinIDs are the dashboards Driga ships. Until the user saves one, its
// spec comes from here.
var BuiltinIDs = []string{HeaderID}

// Builtin returns a shipped dashboard by id.
func Builtin(id string) (Dashboard, bool) {
	if id == HeaderID {
		return Header(), true
	}
	return Dashboard{}, false
}

// Header's metrics follow what people supervising agents need most: what
// waits on them, what finished for review, agents that look stuck, then
// what runaway processes and leftover disk cost.
func Header() Dashboard {
	return Dashboard{
		Name:        "Header",
		Description: "The numbers shown above Chat in the app. Only stat panels.",
		Panels: []Panel{
			{ID: "waiting", Title: "Need you", Viz: "stat", Source: "inbox", Where: []Filter{{Column: "kind", Value: "waiting"}}},
			{ID: "finished", Title: "To review", Viz: "stat", Source: "inbox", Where: []Filter{{Column: "kind", Value: "finished"}}},
			{ID: "stuck", Title: "Possibly stuck", Viz: "stat", Source: "sessions", Where: []Filter{{Column: "label", Value: "stalled?"}}},
			{ID: "leftover", Title: "Leftover processes", Viz: "stat", Source: "orphans", Measure: "sum:rss_bytes"},
			{ID: "reclaim", Title: "Reclaimable", Viz: "stat", Source: "summary", Measure: "sum:reclaim_bytes"},
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
