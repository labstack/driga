package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/labstack/driga/internal/attention"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
	"github.com/labstack/driga/internal/viewer"
)

const flowTab = 7

type flowMsg struct {
	id      int64
	scope   string
	flow    view.FlowView
	details map[int64]view.AgentView
	err     error
}
type flowEventMsg struct{ event view.TailEvent }
type flowHit struct{ x, y, width, height, index int }

func (m *app) loadFlow() tea.Cmd {
	source, ok := m.src.(viewer.FlowSource)
	if !ok {
		return nil
	}
	id, scope, ctx := m.flowID, m.flowScope(), m.opts.Context
	minutes := m.flowZoom()
	scopeKey := m.machineScope
	return func() tea.Msg {
		var f view.FlowView
		var err error
		if window, ok := m.src.(viewer.FlowWindowSource); ok && minutes != 60 {
			f, err = window.LoadFlowWindow(ctx, scope, id, minutes)
		} else {
			f, err = source.LoadFlow(ctx, scope, id)
		}
		return flowMsg{id: id, scope: scopeKey, flow: f, err: err}
	}
}
func (m *app) flowScope() store.Scope {
	scope := store.Scope{}
	if m.machineScope != "" && m.machineScope != "all" {
		id := m.machineScope
		if id == "local" {
			id = m.localMachine()
		}
		scope.Machines = []string{id}
	}
	return scope
}
func (m *app) flowSession(delta int) {
	roots := []view.AgentRow{}
	for _, r := range m.snap.Agents.Rows {
		if m.machineVisible(r.MachineID) && !strings.HasPrefix(r.Origin, "hosted") {
			roots = append(roots, r)
		}
	}
	if len(roots) == 0 {
		return
	}
	i := 0
	if m.flow.Lead != nil {
		for n, r := range roots {
			if r.ID == m.flow.Lead.ID {
				i = n
				break
			}
		}
	}
	i = (i + delta + len(roots)) % len(roots)
	m.flowID = roots[i].ID
	m.flowSelectID = 0
	m.flowExpanded = false
	m.cursors[flowTab] = 0
	m.flowPulse = nil
	m.flowBranchPulse = nil
	m.flowSeen = nil
	m.flowOffset = 0
}
func (m *app) recordFlowEvent(e view.TailEvent) {
	if m.flow.Lead == nil || m.flow.Lead.MachineID != e.MachineID || m.flowSeen[e.Key] {
		return
	}
	nodes := append([]view.FlowNode{*m.flow.Lead}, m.flow.Children...)
	for _, n := range nodes {
		if e.Adapter == n.Adapter && e.ExternalID == n.ExternalID {
			if m.flowPulse == nil {
				m.flowPulse = map[string]time.Time{}
			}
			if m.flowSeen == nil || len(m.flowSeen) > 400 {
				m.flowSeen = map[string]bool{}
			}
			m.flowSeen[e.Key] = true
			m.flowPulse[e.Adapter+":"+e.ExternalID] = m.opts.Now().Add(time.Second)
			return
		}
	}
}
func (m *app) flowPanel(title string, lines []string, width int, selected bool) string {
	width = max(4, width)
	inner := width - 4
	header := "╭─ " + truncate(clean(title), width-6) + " "
	header += strings.Repeat("─", max(0, width-lipgloss.Width(header)-1)) + "╮"
	out := []string{header}
	for _, line := range lines {
		line = strings.ReplaceAll(line, "\t", " ")
		line = truncate(line, inner)
		out = append(out, "│ "+line+strings.Repeat(" ", max(0, inner-lipgloss.Width(line)))+" │")
	}
	out = append(out, "╰"+strings.Repeat("─", width-2)+"╯")
	text := strings.Join(out, "\n")
	if selected && !m.opts.NoColor {
		text = view.TokenStyle("accent", false).Render(text)
	}
	return text
}
func (m *app) flowWorkers() view.FlowWorkers {
	w := m.flow.Workers(0, m.flowExpanded, m.opts.Now())
	if m.flow.Lead != nil {
		w.Nodes = append([]view.FlowNode{*m.flow.Lead}, w.Nodes...)
	}
	return w
}
func (m *app) flowZoom() int {
	if m.flowMinutes == 0 {
		return 60
	}
	return m.flowMinutes
}
func (m *app) flowSelected() *view.FlowNode {
	w := m.flowWorkers()
	i := m.cursors[flowTab]
	if i >= 0 && i < len(w.Nodes) {
		return &w.Nodes[i]
	}
	return nil
}
func (m *app) jumpFlow() tea.Cmd {
	n := m.flowSelected()
	if n == nil {
		return nil
	}
	j := n.Jump
	for _, w := range m.flow.Attention.Waits {
		if w.SessionID == n.ID && w.MachineID == n.MachineID && w.Jump != nil {
			j = w.Jump
			break
		}
	}
	return m.jumpInbox(tabRow{machine: n.MachineID, inbox: &model.InboxItem{Jump: j}})
}
func (m *app) jumpFlowAttention(w view.InboxRow) tea.Cmd {
	jump := m.jumpInbox(tabRow{machine: w.MachineID, inbox: &w.InboxItem})
	m.flowAttention = false
	for i, n := range m.flowWorkers().Nodes {
		if n.ID == w.SessionID && n.MachineID == w.MachineID {
			m.cursors[flowTab] = i
			return jump
		}
	}
	m.flowID = w.SessionID
	m.flowSelectID = w.SessionID
	m.machineScope = w.MachineID
	m.cursors[flowTab] = 0
	return tea.Batch(jump, m.loadFlow())
}

func (m *app) flowDetailLines() []string {
	if m.flowAttention && len(m.flow.Attention.Waits) > 0 {
		w := m.flow.Attention.Waits[min(m.flowAskCursor, len(m.flow.Attention.Waits)-1)]
		return []string{clean(w.Title) + " · " + w.Adapter, "◆ needs you · " + duration(attention.Waited(w.InboxItem, m.opts.Now())), clean(flowWaitReason(w)), "Copy resume: " + clean(attention.JumpCommand(w.Jump)), ""}
	}

	n := m.flowSelected()
	if n == nil {
		return []string{"Select a session · Enter expands quiet threads"}
	}
	state := n.Mixer.Verdict
	if state == "" {
		state = n.Label
	}
	lines := []string{clean(n.DisplayTitle()) + " · " + n.Adapter, view.FlowGlyph(state) + " " + view.FlowStateName(state) + " · as of " + flowAge(n.Mixer.ObservedAt, m.opts.Now()), "why: " + clean(n.Mixer.Evidence), "running " + agentAge(flowElapsed(*n, m.opts.Now()))}
	tools := 0
	for _, e := range n.Events {
		if view.FlowEventMark(n.Adapter, e.Kind) == "tool" {
			tools++
		}
	}
	if n.Adapter != "claude" && n.Adapter != "copilot" {
		lines = append(lines, "no tool events for "+n.Adapter, "no prompt events for "+n.Adapter)
	} else {
		available := tools > 0
		for _, r := range n.Mixer.Rows {
			if (r.Name == "telemetry" || n.Adapter == "copilot" && r.Name == "hooks") && r.Strength > 0 {
				available = true
			}
		}
		if available {
			lines = append(lines, fmt.Sprintf("tool calls %d · retained window", tools))
		} else {
			lines = append(lines, "tool calls — · telemetry unavailable")
		}
	}
	if n.Adapter == "gemini" {
		lines = append(lines, "no wait source for gemini")
	}
	if n.InputTokens != nil {
		lines = append(lines, fmt.Sprintf("input %d tokens", *n.InputTokens))
	}
	if n.OutputTokens != nil {
		lines = append(lines, fmt.Sprintf("output %d tokens", *n.OutputTokens))
	}
	if n.CostUSD != nil {
		lines = append(lines, fmt.Sprintf("cost $%.4f · retained events", *n.CostUSD))
	}
	lines = append(lines, fmt.Sprintf("verdict %s · %d of 4 agree", state, n.Mixer.Agreement))
	for _, r := range n.Mixer.Rows {
		lines = append(lines, clean(r.Name+": "+r.Reading))
	}
	if command := attention.JumpCommand(n.Jump); command != "" {
		lines = append(lines, "Copy resume: "+clean(command))
	}
	if n.Observed != nil && !n.Observed.LastGrowthAt.IsZero() {
		lines = append(lines, "last growth "+transcriptSize(n.Observed.LastGrowthBytes)+" · "+flowAge(n.Observed.LastGrowthAt, m.opts.Now()))
	}
	lines = append(lines, "Recent events · this session family")
	for _, e := range n.Events[max(0, len(n.Events)-4):] {
		lines = append(lines, e.TS.Local().Format("15:04:05")+" "+clean(e.Kind+" "+e.Summary))
	}
	if len(n.Events) == 0 {
		lines = append(lines, "No retained events in window.")
	}
	return lines
}
func flowHeadCap(state string) string {
	switch state {
	case "working":
		return "━━▶"
	case "waiting":
		return "══◆"
	case "idle":
		return "··○"
	case "stalled":
		return "┄┄‖"
	case "finished", "ended", "dead":
		return "──●"
	default:
		return "┈┈?"
	}
}
func flowWaitAge(d time.Duration) string {
	if d >= time.Hour {
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	if d >= time.Minute {
		return fmt.Sprintf("%dm", int(d/time.Minute))
	}
	return fmt.Sprintf("%ds", int(max(0, d)/time.Second))
}
func flowWaitReason(w view.InboxRow) string {
	if w.Reason == "permission" {
		reason := "◆ Permission"
		if w.Tool != "" {
			reason += " · " + w.Tool
		}
		if w.Detail != "" {
			reason += ": " + w.Detail
		}
		return reason
	}
	return "◆ Asked for input · what it asked wasn't captured"
}
func flowAge(at, now time.Time) string {
	if at.IsZero() {
		return "unknown"
	}
	return duration(max(0, now.Sub(at))) + " ago"
}

// flowLayout lays out attention, a bounded map, and the selected session details.
func (m *app) flowLayout() (string, []flowHit) {
	width := max(4, m.width)
	badge := fmt.Sprintf("NEEDS YOU %d", len(m.flow.Attention.Waits))
	if !m.opts.NoColor {
		badge = view.TokenStyle("waiting", false).Reverse(true).Bold(true).Render(badge)
	}
	needs := []string{badge}
	for i, w := range m.flow.Attention.Waits {
		prefix := ""
		if m.flowAttention && i == m.flowAskCursor {
			prefix = "› "
		}
		needs = append(needs, prefix+view.TokenStyle("waiting", m.opts.NoColor).Render("◆ "+flowWaitAge(attention.Waited(w.InboxItem, m.opts.Now())))+"  "+clean(w.Title)+"  "+strings.TrimPrefix(strings.TrimPrefix(clean(flowWaitReason(w)), "◆ Asked for input · "), "◆ "))
	}
	if len(m.flow.Attention.Waits) == 0 {
		needs = append(needs, fmt.Sprintf("Nothing needs you · %d working", m.flow.Attention.Working))
	}
	body := m.flowPanel("Needs you · machine-wide", needs, width, m.flowAttention) + "\n"
	if m.flow.Lead == nil {
		return body + "No live interactive session.", nil
	}
	label := 20
	stateWidth := 0
	if width >= 100 {
		stateWidth = 12
	}
	columns := max(1, width-label-stateWidth-20)
	f := m.flow
	f.ObservedAt = m.opts.Now()
	f.WindowMinutes = m.flowZoom()
	g := view.FlowGeometry(f, columns, m.flowExpanded)
	lines := []string{fmt.Sprintf("%dm · %s–now · head = verdict as of", g.Minutes, g.FromAt.Local().Format("15:04"))}
	if g.EarlierMinutes > 0 {
		lines = append(lines, fmt.Sprintf("%dm earlier · %d asks", g.EarlierMinutes, g.EarlierAsks))
	}
	hits := []flowHit{}
	first := max(0, m.cursors[flowTab]-7)
	first = min(first, max(0, len(g.Rows)-8))
	rowY := strings.Count(body, "\n") + 1 + len(lines)
	for i := first; i < min(len(g.Rows), first+8); i++ {
		r := g.Rows[i]
		n := r.Node
		state := n.Mixer.Verdict
		if state == "" {
			state = n.Label
		}
		prefix := " "
		if i == m.cursors[flowTab] {
			prefix = "›"
		}
		title := truncate(clean(n.DisplayTitle()), label-2)
		title += strings.Repeat(" ", max(0, label-2-lipgloss.Width(title)))
		line := prefix + title + " "
		if width < 60 {
			cap := view.FlowGlyph(state) + " " + view.FlowStateName(state)
			line = prefix + truncate(clean(n.DisplayTitle()), max(0, width-7-lipgloss.Width(cap))) + " · " + cap
		} else {
			if stateWidth > 0 {
				st := view.FlowGlyph(state) + " " + view.FlowStateName(state)
				line += st + strings.Repeat(" ", max(1, stateWidth-lipgloss.Width(st)))
			}
			track := make([]rune, g.Columns)
			cpu := make([]rune, g.Columns)
			for c := range track {
				track[c] = ' '
				cpu[c] = ' '
			}
			last := len(track)
			if n.EndedAt != nil {
				last = max(r.Start, min(last, int(n.EndedAt.Sub(g.FromAt)/time.Minute)+1))
			}
			for c := r.Start; c < last; c++ {
				track[c] = '─'
			}
			for _, h := range r.Hatched {
				for c := h.From; c < min(h.To, len(track)); c++ {
					track[c] = '░'
				}
			}
			var peak, cp int64
			for c, x := range r.Log {
				peak = max(peak, x)
				cp = max(cp, r.CPU[c])
			}
			for c, x := range r.Log {
				if x > 0 {
					track[c] = []rune("▁▂▃▄▅▆▇█")[min(7, int(float64(x)/float64(peak)*7))]
				}
				if r.CPU[c] > 0 && cp > 0 {
					cpu[c] = []rune("▁▂▃▄▅▆▇█")[min(7, int(float64(r.CPU[c])/float64(cp)*7))]
				}
			}
			for _, w := range r.Waits {
				for c := w.From; c < min(w.To, len(track)); c++ {
					track[c] = '═'
				}
			}
			for _, b := range r.Tools {
				track[b.Column] = '┿'
			}
			for _, mark := range r.Marks {
				ch := '◆'
				switch mark.Kind {
				case "prompt":
					ch = '○'
				case "end":
					ch = '●'
				}
				track[mark.Column] = ch
			}
			if n.ParentID != nil && !n.StartedAt.Before(g.FromAt) {
				track[r.Start] = '╰'
				if !m.opts.NoColor && !m.opts.MotionOff && m.opts.Now().Before(m.flowBranchPulse[growthKey(n)]) {
					track[r.Start] = '✦'
				}
			}
			// Parent links run only through otherwise blank cells, including the
			// selected CPU line, and end at the child's own stored start.
			for childIndex, child := range g.Rows {
				if child.Node.ParentID == nil || child.Node.StartedAt.Before(g.FromAt) || childIndex <= i {
					continue
				}
				parentIndex := -1
				for pi, p := range g.Rows {
					if p.Node.ID == *child.Node.ParentID {
						parentIndex = pi
						break
					}
				}
				if parentIndex < 0 || parentIndex > i {
					continue
				}
				c := child.Start
				if parentIndex < i && track[c] == ' ' {
					track[c] = '│'
				}
				if cpu[c] == ' ' {
					cpu[c] = '│'
				}
			}
			cap := flowHeadCap(state)
			if state == "waiting" && !m.opts.NoColor && !m.opts.MotionOff {
				for _, w := range n.Waits {
					age := m.opts.Now().Sub(w.StartedAt)
					if w.EndedAt == nil && age >= 0 && age < 8*time.Second && m.opts.Now().Unix()%4 < 2 {
						cap = "══◇"
					}
				}
			}
			growth := m.flowGrowthPulse[growthKey(n)]
			if !m.opts.NoColor && !m.opts.MotionOff && (m.opts.Now().Before(m.flowPulse[n.Adapter+":"+n.ExternalID]) || m.opts.Now().Before(growth.until)) {
				cap = string([]rune(cap)[:2]) + "◉"
			}
			capRunes := []rune(cap)
			headStart := max(0, len(track)-len(capRunes))
			// Each mark has its own semantic token; the verdict never colours history.
			var rendered strings.Builder
			for _, ch := range track[:headStart] {
				token := "faint"
				switch {
				case strings.ContainsRune("▁▂▃▄▅▆▇█", ch):
					token = "observation"
				case ch == '═' || ch == '◆':
					token = "waiting"
				case ch == '┿':
					token = "tool"
				case ch == '○':
					token = "accent"
				case ch == '●':
					token = "finished"
				}
				rendered.WriteString(view.TokenStyle(token, m.opts.NoColor).Render(string(ch)))
			}
			line += rendered.String() + view.StateStyle(state, m.opts.NoColor).Render(string(capRunes[len(capRunes)-min(len(track), len(capRunes)):])) + view.TokenStyle("faint", m.opts.NoColor).Render(" "+flowAge(n.Mixer.ObservedAt, m.opts.Now()))
			lines = append(lines, line)
			height := 1
			if i == m.cursors[flowTab] {
				height = 2
				lines = append(lines, strings.Repeat(" ", label+stateWidth-4)+view.TokenStyle("faint", m.opts.NoColor).Render("cpu ")+view.TokenStyle("observation", m.opts.NoColor).Faint(!m.opts.NoColor).Render(string(cpu)))
			}
			hits = append(hits, flowHit{x: 0, y: rowY, width: width, height: height, index: i})
			rowY += height
			continue
		}
		lines = append(lines, line)
		hits = append(hits, flowHit{x: 0, y: rowY, width: width, height: 1, index: i})
		rowY++
	}
	if g.Quiet > 0 {
		lines = append(lines, fmt.Sprintf("+%d quiet threads · enter to expand", g.Quiet))
		hits = append(hits, flowHit{x: 0, y: rowY, width: width, height: 1, index: len(m.flowWorkers().Nodes)})
	}
	if len(g.Rows) > 8 {
		lines = append(lines, fmt.Sprintf("rows %d–%d of %d · j/k scroll", first+1, min(first+8, len(g.Rows)), len(g.Rows)))
	}
	if width >= 60 {
		legend := []string{}
		for _, mark := range []struct{ glyph, token, label string }{{"▁", "observation", "growth / CPU"}, {"┿", "tool", "tool"}, {"◆", "waiting", "wait"}, {"○", "accent", "prompt"}, {"●", "finished", "end"}, {"░", "faint", "no data"}} {
			legend = append(legend, view.TokenStyle(mark.token, m.opts.NoColor).Render(mark.glyph)+" "+mark.label)
		}
		lines = append(lines, strings.Join(legend, " · "))
	}
	body += m.flowPanel("Session map", lines, width, false) + "\n" + m.flowPanel("Session detail · signal mixer", m.flowDetailLines(), width, false)
	return body, hits
}
func (m *app) flowView() string          { text, _ := m.flowLayout(); return text }
func (m *app) flowWorkerHits() []flowHit { _, hits := m.flowLayout(); return hits }
func (m *app) flowEnsureVisible()        { m.flowOffset = 0 }

func flowElapsed(n view.FlowNode, now time.Time) time.Duration {
	if n.StartedAt.IsZero() {
		return n.Elapsed
	}
	if n.EndedAt != nil {
		now = *n.EndedAt
	}
	return max(0, now.Sub(n.StartedAt))
}
