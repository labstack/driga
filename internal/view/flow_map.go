package view

import (
	"github.com/labstack/driga/internal/model"
	"time"
)

// FlowWait is an inbox span, including its original start and earliest closure.
type FlowWait struct {
	Tool      string     `json:"-"` // Board-only hint; the existing family/MCP wire contract stays unchanged.
	ID        string     `json:"id"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
	Reason    string     `json:"reason"`
	Detail    string     `json:"detail,omitempty"`
}
type FlowAttention struct {
	Waits   []InboxRow `json:"waits"`
	Working int        `json:"working"`
}

func flowWait(i model.InboxItem) FlowWait {
	start := i.WaitingSinceAt
	if start.IsZero() {
		start = i.TS
	}
	end := i.EndedAt
	if i.AckedAt != nil && (end == nil || i.AckedAt.Before(*end)) {
		end = i.AckedAt
	}
	return FlowWait{ID: i.ID, StartedAt: start, EndedAt: end, Reason: i.Reason, Detail: i.Detail, Tool: i.Tool}
}

// Geometry uses minute-sized terminal cells. No state history is inferred.
type FlowSpan struct{ From, To int }
type FlowToolBin struct{ Column, Count int }
type FlowMark struct {
	Column int
	Kind   string
	At     time.Time
}
type FlowMapRow struct {
	Node     FlowNode
	Start    int
	Hatched  []FlowSpan
	Waits    []FlowSpan
	Tools    []FlowToolBin
	Marks    []FlowMark
	Log, CPU []int64
}
type FlowMap struct {
	FromAt, ToAt                                         time.Time
	Minutes, Columns, EarlierMinutes, EarlierAsks, Quiet int
	Rows                                                 []FlowMapRow
}

func FlowStateName(label string) string {
	switch label {
	case "waiting":
		return "needs you"
	case "stalled":
		return "stalled?"
	case "":
		return "unknown"
	}
	return label
}

func FlowGlyph(label string) string {
	switch label {
	case "working":
		return "▶"
	case "waiting":
		return "◆"
	case "stalled":
		return "‖"
	case "idle":
		return "○"
	case "finished", "ended", "dead":
		return "●"
	}
	return "?"
}

// HeadPattern is a pattern on the current verdict cap, never a historical span.
func FlowHeadPattern(label string) rune {
	switch label {
	case "working":
		return '╼'
	case "waiting":
		return '▒'
	case "stalled":
		return '┄'
	case "idle":
		return '·'
	case "finished", "ended", "dead":
		return '━'
	}
	return '┈'
}
func FlowEventMark(adapter, kind string) string {
	switch adapter {
	case "claude":
		switch kind {
		case "otlp:tool_result":
			return "tool"
		case "otlp:user_prompt":
			return "prompt"
		case "Stop":
			return "end"
		}
	case "copilot":
		switch kind {
		case "postToolUse":
			return "tool"
		case "userPromptSubmitted":
			return "prompt"
		case "agentStop":
			return "end"
		}
	}
	return ""
}
func FlowGeometry(v FlowView, columns int, expanded bool) FlowMap {
	minutes := v.WindowMinutes
	if minutes == 0 {
		minutes = 60
	}
	columns = max(1, min(columns, minutes))
	now := v.ObservedAt
	g := FlowMap{ToAt: now, FromAt: now.Add(-time.Duration(columns) * time.Minute), Minutes: columns, Columns: columns, EarlierMinutes: max(0, minutes-columns), Rows: []FlowMapRow{}}
	col := func(at time.Time) int { return max(0, min(columns-1, int(at.Sub(g.FromAt)/time.Minute))) }
	span := func(from, to time.Time) FlowSpan {
		return FlowSpan{col(from), min(columns, max(col(from)+1, int(to.Sub(g.FromAt)/time.Minute)+1))}
	}
	// Hatching ends exclusively: the first retained minute and the minute
	// after a gap must remain available for their stored measurements.
	hatchSpan := func(from, to time.Time) FlowSpan {
		first := col(from)
		last := int((to.Sub(g.FromAt) + time.Minute - 1) / time.Minute)
		return FlowSpan{first, min(columns, max(first+1, last))}
	}
	workers := v.Workers(0, expanded, now)
	g.Quiet = workers.Quiet
	nodes := workers.Nodes
	if v.Lead != nil {
		nodes = append([]FlowNode{*v.Lead}, nodes...)
	}
	for _, n := range nodes {
		r := FlowMapRow{Node: n, Start: col(n.StartedAt), Log: make([]int64, columns), CPU: make([]int64, columns)}
		end := now
		if n.EndedAt != nil && n.EndedAt.Before(end) {
			end = *n.EndedAt
		}
		if n.RetainedFromAt.IsZero() || n.RetainedFromAt.After(g.FromAt) {
			to := n.RetainedFromAt
			if to.IsZero() {
				to = end
			}
			if to.After(end) {
				to = end
			}
			start := g.FromAt
			if n.StartedAt.After(start) {
				start = n.StartedAt
			}
			if to.Sub(start) >= time.Minute {
				r.Hatched = append(r.Hatched, hatchSpan(start, to))
			}
		}
		for _, gap := range v.Gaps {
			if !gap.To.Before(g.FromAt) && !gap.From.After(now) {
				start := gap.From
				if n.StartedAt.After(start) {
					start = n.StartedAt
				}
				to := gap.To
				if to.After(end) {
					to = end
				}
				if to.After(start) && to.After(g.FromAt) {
					r.Hatched = append(r.Hatched, hatchSpan(start, to))
				}
			}
		}
		for _, w := range n.Waits {
			if w.StartedAt.Before(g.FromAt) {
				g.EarlierAsks++
			}
			end := now
			if w.EndedAt != nil {
				end = *w.EndedAt
			}
			if end.Before(g.FromAt) || end.Before(w.StartedAt) || w.StartedAt.After(now) {
				continue
			}
			r.Waits = append(r.Waits, span(w.StartedAt, end))
			r.Marks = append(r.Marks, FlowMark{col(w.StartedAt), "wait", w.StartedAt})
		}
		bins := make([]int, columns)
		for _, e := range n.Events {
			if e.TS.Before(g.FromAt) || e.TS.After(now) {
				continue
			}
			kind := FlowEventMark(n.Adapter, e.Kind)
			if kind == "tool" {
				bins[col(e.TS)]++
			} else if kind != "" {
				r.Marks = append(r.Marks, FlowMark{col(e.TS), kind, e.TS})
			}
		}
		for i, count := range bins {
			if count > 0 {
				r.Tools = append(r.Tools, FlowToolBin{i, count})
			}
		}
		for _, h := range n.Handoffs {
			if !h.TS.Before(g.FromAt) && !h.TS.After(now) {
				r.Marks = append(r.Marks, FlowMark{col(h.TS), "end", h.TS})
			}
		}
		if n.Observed != nil {
			for i, x := range n.Observed.LogBytesPerMin {
				at := n.Observed.FromAt.Add(time.Duration(i) * time.Minute)
				if at.Before(g.FromAt) || at.Before(n.StartedAt) || at.After(end) {
					continue
				}
				c := col(at)
				missing := false
				for _, h := range r.Hatched {
					if c >= h.From && c < h.To {
						missing = true
						break
					}
				}
				if missing {
					continue
				}
				r.Log[c] += x
				if i < len(n.Observed.CPUMSPerMin) {
					r.CPU[c] += n.Observed.CPUMSPerMin[i]
				}
			}
		}
		g.Rows = append(g.Rows, r)
	}
	return g
}
