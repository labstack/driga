package view

import (
	"slices"
	"strings"
)

// HeaderStats is the shared attention and hygiene contract for the app and TUI.
// Review counts one unread completion per session, with unbound items separate.
type HeaderStats struct {
	NeedYou, ToReview, Stuck, Leftover                  int
	LeftoverBytes, Reclaimable, MemoryUsed, MemoryTotal uint64
}

func HeaderCounts(inbox InboxView, agents AgentsView, orphans OrphansView, summary SummaryView) HeaderStats {
	h := HeaderStats{Leftover: len(orphans.Rows), Reclaimable: summary.Reclaim, MemoryUsed: summary.Sample.MemUsed, MemoryTotal: summary.Sample.MemTotal}
	needs, unbound := needYouSessions(inbox, agents)
	h.NeedYou = len(needs) + unbound
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

type headerSession struct {
	machine string
	id      int64
}

// needYouSessions combines session state and inbox attention. Unbound inbox
// items remain individual needs; all other evidence counts once per session.
func needYouSessions(inbox InboxView, agents AgentsView) (map[headerSession]bool, int) {
	needs := map[headerSession]bool{}
	unbound := 0
	for _, row := range inbox.Rows {
		if row.Historical || row.Kind == "finished" || row.Kind == "worker_finished" || row.Kind != "waiting" && row.EndedAt != nil {
			continue
		}
		if row.SessionID == 0 {
			unbound++
		} else {
			needs[headerSession{row.MachineID, row.SessionID}] = true
		}
	}
	var walk func([]AgentRow)
	walk = func(rows []AgentRow) {
		for _, row := range rows {
			if strings.TrimPrefix(row.Label, "~") == "waiting" {
				needs[headerSession{row.MachineID, row.ID}] = true
			}
			walk(row.Children)
		}
	}
	walk(agents.Rows)
	return needs, unbound
}

// WaitingSessions identifies sessions already counted as needing the user on
// one machine, so a live approval does not add another need for that session.
func WaitingSessions(inbox InboxView, agents AgentsView, machine string) []int64 {
	var ids []int64
	needs, _ := needYouSessions(inbox, agents)
	for session := range needs {
		if session.machine == machine && session.id > 0 {
			ids = append(ids, session.id)
		}
	}
	slices.Sort(ids)
	return ids
}
func ExtraPrompts(inbox InboxView, agents AgentsView, machine string, sessions []int64) int {
	waiting := map[int64]bool{}
	for _, id := range WaitingSessions(inbox, agents, machine) {
		waiting[id] = true
	}
	extra := 0
	for _, id := range sessions {
		if id == 0 || !waiting[id] {
			extra++
			waiting[id] = true
		}
	}
	return extra
}
