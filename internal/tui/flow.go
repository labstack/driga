package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/labstack/driga/internal/derive"
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
	scopeKey := m.machineScope
	return func() tea.Msg {
		f, err := source.LoadFlow(ctx, scope, id)
		details := map[int64]view.AgentView{}
		if err == nil && f.Lead != nil {
			if s, ok := source.(viewer.DetailSource); ok {
				nodes := append([]view.FlowNode{*f.Lead}, f.Children...)
				for _, n := range nodes {
					d, e := s.LoadAgent(ctx, store.Scope{Machines: []string{n.MachineID}}, n.ID)
					if e != nil {
						return flowMsg{id: id, scope: scopeKey, err: e}
					}
					details[n.ID] = d
				}
			}
		}
		return flowMsg{id: id, scope: scopeKey, flow: f, details: details, err: err}
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
	m.flowExpanded = false
	m.cursors[flowTab] = 0
	m.flowPulse = nil
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
func (m *app) flowWire(n view.FlowNode) string {
	if until := m.flowPulse[n.Adapter+":"+n.ExternalID]; m.opts.Now().Before(until) {
		return "──●──▶"
	}
	return "─────▶"
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
func (m *app) flowLeadLines() []string {
	n := m.flow.Lead
	if n == nil {
		return []string{"No live interactive session."}
	}
	state := n.Label
	if n.Heuristic {
		state = "~" + state
	}
	if n.Label == "stalled" {
		state += "?"
	}
	lines := []string{clean(n.DisplayTitle()), view.StateStyle(n.Label, m.opts.NoColor).Render("● " + state), "project " + clean(view.FlowJoin(n.Project, n.Branch)), "age     " + agentAge(n.Age)}
	if n.Model != "" {
		lines = append(lines, "model   "+clean(n.Model))
	}
	if n.InputTokens != nil {
		lines = append(lines, fmt.Sprintf("input   %d tokens", *n.InputTokens))
	}
	if n.OutputTokens != nil {
		lines = append(lines, fmt.Sprintf("output  %d tokens", *n.OutputTokens))
	}
	if n.CostUSD != nil {
		lines = append(lines, fmt.Sprintf("cost    $%.4f · retained events", *n.CostUSD))
	}
	if n.LastTool != "" {
		lines = append(lines, "tool    "+clean(n.LastTool))
	}
	if n.Mem != nil {
		lines = append(lines, fmt.Sprintf("memory  %s · %d helpers", humanBytes(*n.Mem), *n.Helpers))
	}
	return lines
}
func (m *app) flowMixerLines() []string {
	lines := []string{}
	for _, r := range m.flow.Mixer.Rows {
		filled := min(12, max(0, int(r.Strength*12+.5)))
		bars := strings.Repeat("▮", filled) + strings.Repeat("▯", 12-filled)
		token := "idle"
		if r.Supports {
			token = "working"
		}
		lines = append(lines, fmt.Sprintf("%-9s %s", r.Name, view.TokenStyle(token, m.opts.NoColor).Render(bars)), "          "+clean(r.Reading))
		if !r.At.IsZero() {
			lines[len(lines)-1] += " · " + duration(r.Age) + " ago"
		}
	}
	lines = append(lines, fmt.Sprintf("verdict %s · %d of 4 agree", m.flow.Mixer.Verdict, m.flow.Mixer.Agreement))
	if m.flow.Mixer.Heuristic {
		lines = append(lines, "heuristic")
	}
	if m.flow.Mixer.Evidence != "" {
		lines = append(lines, clean(m.flow.Mixer.Evidence))
	}
	return lines
}
func (m *app) flowWorkerPanel(n view.FlowNode, i, width int) string {
	kind := n.Kind
	if kind == "" {
		kind = "worker"
	}
	title := view.FlowJoin(kind, n.Adapter)
	activity := "activity unavailable"
	if !n.LastActivityAt.IsZero() {
		activity = view.FlowJoin(clean(n.LastActivity), duration(m.opts.Now().Sub(n.LastActivityAt))+" ago")
	}
	if n.LastTool != "" {
		activity = view.FlowJoin(clean(n.LastTool), activity)
	}
	state := view.StateStyle(n.Label, m.opts.NoColor).Render(derive.Display(n.Label, n.Heuristic))
	titleText := truncate(clean(n.DisplayTitle()), max(0, width-7-lipgloss.Width(state)))
	spark := view.FlowSpark(n.Activity)
	elapsed := agentAge(n.Elapsed)
	if strings.TrimSpace(spark) != "" {
		elapsed = view.TokenStyle("idle", m.opts.NoColor).Render(spark) + " · " + elapsed
	}
	lines := []string{view.FlowJoin(titleText, state), activity, elapsed}

	return m.flowPanel(title, lines, width, m.cursors[flowTab] == i)
}

// flowWorkers reserves room for the shared panels, tail and summary. Expanded
// families remain scrollable so every thread can still be opened.
func (m *app) flowWorkers() view.FlowWorkers {
	top := max(len(m.flowLeadLines()), len(m.flowMixerLines())) + 2
	available := m.bodyHeight() - 9
	if m.width < 150 {
		if m.width < 110 {
			top = len(m.flowLeadLines()) + len(m.flowMixerLines()) + 5
		}
		available -= top + 1
	}
	return m.flow.Workers(max(1, (available-3)/6), m.flowExpanded, m.opts.Now())
}

// flowLayout produces both the view and hit regions from the same geometry.
func (m *app) flowLayout() (string, []flowHit) {
	if m.flow.Lead == nil {
		return truncate("No live interactive session. Select an agent, then press 7.", max(0, m.width)), nil
	}
	if m.width < 12 {
		return truncate("Widen terminal for flow", max(0, m.width)), nil
	}
	width := m.width
	visible := m.flowWorkers()
	hits := []flowHit{}

	summary := func(w int) string {
		return m.flowPanel(fmt.Sprintf("+%d quiet threads", visible.Quiet), []string{"Enter / click to expand"}, w, m.cursors[flowTab] == len(visible.Nodes))
	}
	body := ""
	// Draw the branch rail once; each event marker belongs to its worker only.
	branchStack := func(panelWidth, prefixWidth, x, y int, wide bool) string {
		panels := []string{}
		for i, n := range visible.Nodes {
			panels = append(panels, m.flowWorkerPanel(n, i, panelWidth))
		}
		if visible.Quiet > 0 {
			panels = append(panels, summary(panelWidth))
		}
		out := []string{}
		for i, panel := range panels {
			lines := strings.Split(panel, "\n")
			hits = append(hits, flowHit{x: x, y: y + len(out), width: panelWidth, height: len(lines), index: i})
			for j, line := range lines {
				wire := "  │" + strings.Repeat(" ", prefixWidth-3)
				if wide && i == 0 && j < 2 || i == len(panels)-1 && j > 2 {
					wire = strings.Repeat(" ", prefixWidth)
				}
				if j == 2 {
					joint := "├"
					if i == len(panels)-1 {
						joint = "└"
					}
					if wide && i == 0 {
						joint = "┬"
					}
					wire = "  " + joint + strings.Repeat("─", prefixWidth-4) + "▶"
					if wide && i == 0 {
						wire = "──" + wire[2:]
					}
					if i < len(visible.Nodes) {
						n := visible.Nodes[i]
						if m.opts.Now().Before(m.flowPulse[n.Adapter+":"+n.ExternalID]) {
							wire = "  " + joint + "──●" + strings.Repeat("─", prefixWidth-7) + "▶"
							if wide && i == 0 {
								wire = "──" + wire[2:]
							}
						}
					}
				}
				out = append(out, wire+line)
			}
			if i < len(panels)-1 {
				out = append(out, "  │"+strings.Repeat(" ", prefixWidth-3+panelWidth))
			}
		}
		return strings.Join(out, "\n")
	}
	leadPanel := func(w int) string { return m.flowPanel("lead · "+m.flow.Lead.Adapter, m.flowLeadLines(), w, true) }
	mixerPanel := func(w int) string {
		return m.flowPanel("signal mixer · why: "+m.flow.Mixer.Verdict, m.flowMixerLines(), w, false)
	}
	if width >= 150 {
		col := (width - 16) / 3
		lead, mixer := leadPanel(col), mixerPanel(col)
		wire := []string{}
		for i := 0; i < max(lipgloss.Height(lead), lipgloss.Height(mixer)); i++ {
			w := "      "
			if i == 2 {
				w = m.flowWire(*m.flow.Lead)
			}
			wire = append(wire, w)
		}
		body = lipgloss.JoinHorizontal(lipgloss.Top, lead, strings.Join(wire, "\n"), mixer, branchStack(width-2*col-16, 10, 2*col+16, 0, true))
	} else {
		mixerX := 2
		if width >= 110 {
			col := (width - 6) / 2
			lead, mixer := leadPanel(col), mixerPanel(width-col-6)
			wire := []string{}
			for i := 0; i < max(lipgloss.Height(lead), lipgloss.Height(mixer)); i++ {
				w := "      "
				if i == 2 {
					w = m.flowWire(*m.flow.Lead)
				}
				wire = append(wire, w)
			}
			body = lipgloss.JoinHorizontal(lipgloss.Top, lead, strings.Join(wire, "\n"), mixer)
			mixerX = col + 8
		} else {
			body = leadPanel(width) + "\n  │\n  ▼\n" + mixerPanel(width)
		}
		if len(visible.Nodes) > 0 || visible.Quiet > 0 {
			if width >= 110 {
				body += "\n  ┌" + strings.Repeat("─", max(0, mixerX-3)) + "┘\n"
			} else {
				body += "\n  │\n"
			}
			y := lipgloss.Height(body) - 1
			body += branchStack(width-8, 8, 8, y, false)
		}
	}
	tail := []string{}
	for _, e := range m.flow.Tail {
		tail = append(tail, e.TS.Local().Format("15:04:05")+" "+clean(e.Adapter)+" "+clean(e.Kind)+" "+clean(e.Summary))
	}
	if len(tail) > 5 {
		tail = tail[len(tail)-5:]
	}
	if len(tail) == 0 {
		tail = []string{"No stored events."}
	}
	body += "\n\n" + m.flowPanel("tail -f · this session family", tail, width, false)
	return body, hits
}
func (m *app) flowView() string          { text, _ := m.flowLayout(); return text }
func (m *app) flowWorkerHits() []flowHit { _, hits := m.flowLayout(); return hits }

func (m *app) flowEnsureVisible() {
	for _, h := range m.flowWorkerHits() {
		if h.index == m.cursors[flowTab] {
			if h.y < m.flowOffset {
				m.flowOffset = h.y
			}
			if h.y+h.height > m.flowOffset+m.bodyHeight() {
				m.flowOffset = max(0, h.y+h.height-m.bodyHeight())
			}
			return
		}
	}
}
