package view

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/labstack/driga/internal/attention"
	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/model"
)

// HeaderStats is the shared attention and hygiene contract for the app and TUI.
// Review counts one unread completion per session, with unbound items separate.
type HeaderStats struct {
	NeedYou, ToReview, Stuck, Leftover     int
	LeftoverBytes, MemoryUsed, MemoryTotal uint64
}

func (a AttentionProjection) Header(orphans OrphansView, summary SummaryView) HeaderStats {
	inbox, agents := a.inbox, a.agents
	h := HeaderStats{Leftover: len(orphans.Rows), MemoryUsed: summary.Sample.MemUsed, MemoryTotal: summary.Sample.MemTotal}
	h.NeedYou = len(a.needs) + a.unbound
	reviewed := map[headerSession]bool{}
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
			k := headerSession{row.MachineID, row.SessionID}
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

// livePrompt contains only the in-memory facts needed for attention. It is
// never written into the collected snapshot or durable history.
type livePrompt struct {
	ID, MachineID, Tool, Detail string
	SessionID                   int64
	CreatedAt                   time.Time
}

// AttentionProjection owns identity and reconciliation for header and Flow.
// Destinations keep their inclusion policies: warnings count in the header,
// while Flow shows waits for known, live sessions only.
type AttentionProjection struct {
	inbox   InboxView
	agents  AgentsView
	needs   map[headerSession]bool
	unbound int
	prompts []livePrompt
}

func ProjectAttention(inbox InboxView, agents AgentsView, machine string, pending []chat.Prompt) AttentionProjection {
	prompts := make([]livePrompt, 0, len(pending))
	for _, p := range pending {
		prompts = append(prompts, livePrompt{ID: p.ID, MachineID: machine, SessionID: p.SessionID, Tool: p.Tool, Detail: p.Detail(), CreatedAt: p.CreatedAt})
	}
	needs, unbound := needYouSessions(inbox, agents)
	for _, p := range prompts {
		if p.SessionID == 0 {
			unbound++
		} else {
			needs[headerSession{p.MachineID, p.SessionID}] = true
		}
	}
	return AttentionProjection{inbox: inbox, agents: agents, needs: needs, unbound: unbound, prompts: prompts}
}

func (k headerSession) String() string { return fmt.Sprintf("%s/%d", k.machine, k.id) }

// Board overlays prompts on a copy. Neither persisted evidence nor another
// destination's snapshot can retain a prompt after its hook is answered.
func (a AttentionProjection) Board(data BoardSnapshot, window time.Duration, now time.Time) BoardView {
	data.Nodes = slices.Clone(data.Nodes)
	prompts := map[string]livePrompt{}
	for _, p := range a.prompts {
		prompts[headerSession{p.MachineID, p.SessionID}.String()] = p
		for i := range data.Nodes {
			n := &data.Nodes[i]
			if n.ID == p.SessionID && n.MachineID == p.MachineID && n.EndedAt == nil {
				n.Waits = append(slices.Clone(n.Waits), FlowWait{ID: p.ID, StartedAt: p.CreatedAt, Tool: p.Tool, Detail: p.Detail, Reason: "permission"})
			}
		}
	}
	b := Board(data, window, now)
	annotate := func(r *BoardRow) {
		if p, ok := prompts[r.Key]; ok && r.Node.EndedAt == nil {
			r.PromptID = p.ID
			r.Command = p.Detail
		}
	}
	for gi := range b.Groups {
		for ri := range b.Groups[gi].Rows {
			annotate(&b.Groups[gi].Rows[ri])
		}
	}
	for i := range b.Needs {
		annotate(&b.Needs[i])
	}
	return b
}

// liveSessionWait is the shared inbox and board rule for a wait needing the user.
func liveSessionWait(item model.InboxItem, s model.Session, known bool, now time.Time) bool {
	return attention.Active(item) && known && s.EndedAt == nil && s.Label == "waiting" && len(visibleSessions([]model.Session{s}, false, now)) > 0
}
