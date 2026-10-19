package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
)

func flowFixture() view.FlowView {
	return view.FlowView{Lead: &view.FlowNode{ID: 1, MachineID: "local", ExternalID: "lead", Adapter: "claude", Title: "Lead", Label: "working"}, Mixer: model.StateMixer{Verdict: "working", Rows: []model.StateSignal{{Name: "registry", Reading: "busy", Strength: 1}, {Name: "hooks", Reading: "Stop", Strength: .3}, {Name: "telemetry", Reading: "1 / min", Strength: .5}, {Name: "process", Reading: "alive", Strength: 1}}}, Children: []view.FlowNode{{ID: 2, MachineID: "local", ExternalID: "worker", Adapter: "codex", Title: strings.Repeat("worker ", 40), Label: "working", Activity: []int{0, 1, 2, 3, 4, 5, 0, 0, 1, 0}}, {ID: 3, ExternalID: "hosted", Kind: "hosted", Title: "Hosted", Label: "idle", LastActivityAt: time.Now()}, {ID: 4, Title: "Finished", Label: "finished"}}, Tail: []view.TailEvent{{Key: "old", MachineID: "local", Adapter: "claude", ExternalID: "lead", Summary: "family only"}}}
}
func TestFlowWidthsAndPulse(t *testing.T) {
	now := time.Now()
	m := setup(t, fixture(), Options{Now: func() time.Time { return now }}).(*app)
	m.tab = flowTab
	m.flow = flowFixture()
	m.loaded = true
	for _, width := range []int{160, 120, 80} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 44})
		text := m.flowView()
		if lipgloss.Height(m.View().Content) > 44 {
			t.Fatalf("%d overflows terminal height", width)
		}
		for _, line := range strings.Split(m.View().Content, "\n") {
			if lipgloss.Width(line) > width {
				t.Fatalf("%d full view overflow", width)
			}
		}
		if width == 160 && !strings.Contains(m.View().Content, "this session family") {
			t.Fatal("tail hidden at 160x44")
		}
		for _, line := range strings.Split(text, "\n") {
			if lipgloss.Width(line) > width {
				t.Fatalf("%d: %q", width, line)
			}
		}
		for _, want := range []string{"lead", "signal mixer", "registry", "hooks", "telemetry", "process", "this session family"} {
			if !strings.Contains(text, want) {
				t.Errorf("%d missing %s", width, want)
			}
		}
		if strings.Contains(text, "──●") {
			t.Fatal("pulse without event")
		}
	}
	m.width = 160
	m.Update(flowEventMsg{view.TailEvent{Key: "new", MachineID: "other", Adapter: "codex", ExternalID: "worker"}})
	if strings.Contains(m.flowView(), "──●") {
		t.Fatal("pulse from another machine")
	}
	m.Update(flowEventMsg{view.TailEvent{Key: "new", MachineID: "local", Adapter: "codex", ExternalID: "worker"}})
	if !strings.Contains(m.flowView(), "──●") {
		t.Fatal("missing event pulse")
	}
	now = now.Add(2 * time.Second)
	if strings.Contains(m.flowView(), "──●") {
		t.Fatal("pulse did not expire")
	}
}
func TestFlowWorkerKeysAndMouse(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	m.tab = flowTab
	m.flow = flowFixture()
	m.width = 160
	m.height = 44
	m.loaded = true
	m.rebuild()
	m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if m.cursors[flowTab] != 1 {
		t.Fatal(m.cursors)
	}
	m.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	if m.cursors[flowTab] != 0 {
		t.Fatal(m.cursors)
	}
	hits := m.flowWorkerHits()
	if len(hits) != 3 {
		t.Fatal(hits)
	}
	h := hits[1]
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: h.x, Y: h.y + lipgloss.Height(m.header()) + lipgloss.Height(m.tabBar()) + 1})
	if m.cursors[flowTab] != 1 {
		t.Fatal("click did not select worker")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.detail || m.detailRow == nil || m.detailRow.agent != 3 {
		t.Fatal("Enter did not open selected worker")
	}
}

func TestFlowRefreshKeepsSelectionAndOnlyNewEventsPulse(t *testing.T) {
	now := time.Now()
	m := setup(t, fixture(), Options{Now: func() time.Time { return now }}).(*app)
	m.tab = flowTab
	m.flow = flowFixture()
	m.flowID = 1
	m.width, m.height = 160, 44
	m.cursors[flowTab] = 1
	m.loaded = true
	next := flowFixture()
	next.Children[0], next.Children[1] = next.Children[1], next.Children[0]
	m.Update(flowMsg{id: 1, flow: next})
	if m.flowWorkers().Nodes[m.cursors[flowTab]].ID != 3 {
		t.Fatal("worker selection changed during reorder")
	}
	snapshot := m.snap
	snapshot.Tail = []view.TailEvent{{Key: "one", MachineID: "local", Adapter: "claude", ExternalID: "lead"}}
	m.snap.Tail = snapshot.Tail
	m.Update(refreshMsg{changed: true, snap: snapshot})
	if len(m.flowPulse) != 0 {
		t.Fatal("refresh alone pulsed")
	}
	snapshot.Tail = append(snapshot.Tail, view.TailEvent{Key: "two", MachineID: "local", Adapter: "claude", ExternalID: "lead"})
	m.Update(refreshMsg{changed: true, snap: snapshot})
	if len(m.flowPulse) != 1 {
		t.Fatal("new event did not pulse")
	}
}

func TestFlowEntryUsesSelectedAgentAndSessionKeys(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	m.tab = 0
	m.rows = []tabRow{{agent: 42, cells: []string{"", "working", "selected"}}}
	m.cursors[0] = 0
	m.Update(tea.KeyPressMsg{Code: '7', Text: "7"})
	if m.tab != flowTab || m.flowID != 42 {
		t.Fatal("Flow ignored selected agent")
	}
	m.flow = flowFixture()
	m.snap.Agents.Rows = []view.AgentRow{{ID: 1}, {ID: 9}}
	m.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
	if m.flowID != 9 {
		t.Fatal("next session did not switch")
	}
	m.flow.Lead.ID = 9
	m.Update(tea.KeyPressMsg{Code: '[', Text: "["})
	if m.flowID != 1 {
		t.Fatal("previous session did not switch")
	}
}

func TestFlowQuietSummaryExpands(t *testing.T) {
	now := time.Now()
	m := setup(t, fixture(), Options{NoColor: true, Now: func() time.Time { return now }}).(*app)
	m.tab, m.width, m.height, m.loaded = flowTab, 160, 44, true
	m.flow = flowFixture()
	for i := 0; i < 11; i++ {
		m.flow.Children = append(m.flow.Children, view.FlowNode{ID: int64(100 + i), Adapter: "codex", Kind: "hosted", Label: "idle", Title: "quiet worker", LastActivityAt: now.Add(-2 * time.Hour)})
	}
	text, hits := m.flowLayout()
	if !strings.Contains(text, "quiet threads") || strings.Contains(text, "quiet worker") {
		t.Fatal("quiet hosted threads must start collapsed")
	}
	if len(hits) > 5 {
		t.Fatal("too many panels for terminal", hits)
	}
	m.cursors[flowTab] = hits[len(hits)-1].index
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.detail || !strings.Contains(m.flowView(), "quiet worker") {
		t.Fatal("Enter must expand summary")
	}
	m.flowExpanded = false
	m.rebuild()
	hits = m.flowWorkerHits()
	h := hits[len(hits)-1]
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: h.x, Y: h.y + lipgloss.Height(m.header()) + lipgloss.Height(m.tabBar()) + 1})
	if !m.flowExpanded {
		t.Fatal("click must expand summary")
	}
}

func TestFlowConnectedBranchesAndReason(t *testing.T) {
	now := time.Now()
	m := setup(t, fixture(), Options{NoColor: true, Now: func() time.Time { return now }}).(*app)
	m.tab, m.width, m.height, m.loaded = flowTab, 160, 50, true
	m.flow = flowFixture()
	m.flow.Mixer.Evidence = "because registry: busy"
	m.Update(flowEventMsg{view.TailEvent{Key: "branch", MachineID: "local", Adapter: "codex", ExternalID: "worker"}})
	text, hits := m.flowLayout()
	lines := strings.Split(text, "\n")
	if !strings.Contains(text, "because registry: busy") {
		t.Error("missing deciding signal")
	}
	if !strings.Contains(text, "├") || !strings.Contains(text, "└") {
		t.Error("missing connected trunk branches")
	}
	for _, h := range hits {
		line := lines[h.y+2]
		if !strings.Contains(lines[h.y+1], m.flowWorkers().Nodes[h.index].Label) {
			t.Error("long worker title hides state")
		}
		if strings.Contains(line, "──●") != (h.index == 0) {
			t.Errorf("event marker on wrong worker branch %d: %s", h.index, line)
		}
	}
	for _, width := range []int{160, 120, 80, 40, 20, 8} {
		m.width = width
		for _, line := range strings.Split(m.flowView(), "\n") {
			if lipgloss.Width(line) > width {
				t.Errorf("overflow at %d", width)
			}
		}
	}
}
