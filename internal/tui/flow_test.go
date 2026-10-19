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
	return view.FlowView{Lead: &view.FlowNode{ID: 1, MachineID: "local", ExternalID: "lead", Adapter: "claude", Title: "Lead", Label: "working"}, Mixer: model.StateMixer{Verdict: "working", Rows: []model.StateSignal{{Name: "registry", Reading: "busy", Strength: 1}, {Name: "hooks", Reading: "Stop", Strength: .3}, {Name: "telemetry", Reading: "1 / min", Strength: .5}, {Name: "process", Reading: "alive", Strength: 1}}}, Children: []view.FlowNode{{ID: 2, MachineID: "local", ExternalID: "worker", Adapter: "codex", Title: strings.Repeat("worker ", 40), Label: "working", Activity: []int{0, 1, 2, 3, 4, 5, 0, 0, 1, 0}}, {ID: 3, ExternalID: "hosted", Kind: "hosted", Title: "Hosted", Label: "idle"}, {ID: 4, Title: "Finished", Label: "finished"}}, Tail: []view.TailEvent{{Key: "old", MachineID: "local", Adapter: "claude", ExternalID: "lead", Summary: "family only"}}}
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
		for _, want := range []string{"lead", "signal mixer", "registry", "hooks", "telemetry", "process", "Hosted", "Finished", "this session family"} {
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
	m.cursors[flowTab] = 1
	m.loaded = true
	next := flowFixture()
	next.Children[0], next.Children[1] = next.Children[1], next.Children[0]
	m.Update(flowMsg{id: 1, flow: next})
	if m.cursors[flowTab] != 0 {
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
