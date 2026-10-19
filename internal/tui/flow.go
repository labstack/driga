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
	lines := []string{clean(n.Title), view.StateStyle(n.Label, m.opts.NoColor).Render("● " + state), "project " + clean(n.Project) + " · " + clean(n.Branch), "age     " + agentAge(n.Age)}
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
	return lines
}
func (m *app) flowWorkerPanel(i, width int) string {
	n := m.flow.Children[i]
	kind := n.Kind
	if kind == "" {
		kind = "worker"
	}
	title := kind + " · " + n.Adapter
	activity := "activity unavailable"
	if !n.LastActivityAt.IsZero() {
		activity = clean(n.LastActivity) + " · " + duration(m.opts.Now().Sub(n.LastActivityAt)) + " ago"
	}
	if n.LastTool != "" {
		activity = clean(n.LastTool) + " · " + activity
	}
	lines := []string{clean(n.Title) + " · " + view.StateStyle(n.Label, m.opts.NoColor).Render(derive.Display(n.Label, n.Heuristic)), activity, view.TokenStyle("idle", m.opts.NoColor).Render(view.FlowSpark(n.Activity)) + " · " + agentAge(n.Elapsed)}

	return m.flowPanel(title, lines, width, m.cursors[flowTab] == i)
}

// flowLayout produces both the view and hit regions from the same geometry.
func (m *app) flowLayout() (string, []flowHit) {
	if m.flow.Lead == nil {
		return "No live interactive session. Select an agent, then press 7.", nil
	}
	width := m.width
	hits := []flowHit{}
	body := ""
	if width >= 150 {
		col := (width - 16) / 3
		workerWidth := width - 2*col - 16
		lead := m.flowPanel("lead · "+m.flow.Lead.Adapter, m.flowLeadLines(), col, true)
		mixer := m.flowPanel("signal mixer · why: "+m.flow.Mixer.Verdict, m.flowMixerLines(), col, false)
		workers := []string{}
		for i, n := range m.flow.Children {
			p := strings.Split(m.flowWorkerPanel(i, workerWidth), "\n")
			for j := range p {
				wire := "      "
				if j == 2 {
					wire = m.flowWire(n)
				}
				p[j] = wire + p[j]
			}
			hits = append(hits, flowHit{x: 2*col + 16, y: i * 6, width: workerWidth, height: 5, index: i})
			workers = append(workers, strings.Join(p, "\n"))
		}
		leadWire := []string{}
		for i := 0; i < max(lipgloss.Height(lead), lipgloss.Height(mixer)); i++ {
			w := "      "
			if i == 2 {
				w = m.flowWire(*m.flow.Lead)
			}
			leadWire = append(leadWire, w)
		}
		body = lipgloss.JoinHorizontal(lipgloss.Top, lead, strings.Join(leadWire, "\n"), mixer, "    ", strings.Join(workers, "\n\n"))
	} else {
		if width >= 110 {
			col := (width - 6) / 2
			body = lipgloss.JoinHorizontal(lipgloss.Top, m.flowPanel("lead · "+m.flow.Lead.Adapter, m.flowLeadLines(), col, true), " ──▶ ", m.flowPanel("signal mixer · why: "+m.flow.Mixer.Verdict, m.flowMixerLines(), width-col-5, false))
		} else {
			body = m.flowPanel("lead · "+m.flow.Lead.Adapter, m.flowLeadLines(), width, true) + "\n" + m.flowPanel("signal mixer · why: "+m.flow.Mixer.Verdict, m.flowMixerLines(), width, false)
		}
		// At 120/80 columns workers stack beneath the lead and mixer.
		for i, n := range m.flow.Children {
			body += "\n" + m.flowWire(n) + "\n"
			y := lipgloss.Height(body) - 1
			hits = append(hits, flowHit{x: 0, y: y, width: width, height: 5, index: i})
			body += m.flowWorkerPanel(i, width)
		}
	}
	body += "\n" + truncate("╰──── results return to lead ────╯", width)
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
