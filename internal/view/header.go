package view

import "strings"

// HeaderStats is the shared attention and hygiene contract for the app and TUI.
// Review counts one unread completion per session, with unbound items separate.
type HeaderStats struct {
	NeedYou, ToReview, Stuck, Leftover                  int
	LeftoverBytes, Reclaimable, MemoryUsed, MemoryTotal uint64
}

func HeaderCounts(inbox InboxView, agents AgentsView, orphans OrphansView, summary SummaryView) HeaderStats {
	h := HeaderStats{Leftover: len(orphans.Rows), Reclaimable: summary.Reclaim, MemoryUsed: summary.Sample.MemUsed, MemoryTotal: summary.Sample.MemTotal}
	type key struct {
		machine string
		id      int64
	}
	reviewed := map[key]bool{}
	for _, row := range inbox.Rows {
		if row.Historical {
			continue
		}
		switch row.Kind {
		case "finished":
			if row.SessionID == 0 {
				h.ToReview++
				continue
			}
			k := key{row.MachineID, row.SessionID}
			if !reviewed[k] {
				h.ToReview++
				reviewed[k] = true
			}
		case "worker_finished":
		case "waiting":
			h.NeedYou++
		default:
			if row.EndedAt == nil {
				h.NeedYou++
			}
		}
	}
	var walk func([]AgentRow)
	walk = func(rows []AgentRow) {
		for _, row := range rows {
			if strings.TrimPrefix(row.Label, "~") == "stuck" {
				h.Stuck++
			}
			walk(row.Children)
		}
	}
	walk(agents.Rows)
	for _, row := range orphans.Rows {
		h.LeftoverBytes += row.RSSTotal
	}
	return h
}

// WaitingSessions identifies active waits on one machine, allowing a live
// approval card and its durable notification marker to count once.
func WaitingSessions(inbox InboxView, machine string) []int64 {
	var ids []int64
	for _, row := range inbox.Rows {
		if row.MachineID == machine && row.Kind == "waiting" && !row.Historical && row.SessionID > 0 {
			ids = append(ids, row.SessionID)
		}
	}
	return ids
}
func ExtraPrompts(inbox InboxView, machine string, sessions []int64) int {
	waiting := map[int64]bool{}
	for _, id := range WaitingSessions(inbox, machine) {
		waiting[id] = true
	}
	extra := 0
	for _, id := range sessions {
		if id == 0 || !waiting[id] {
			extra++
		}
	}
	return extra
}
