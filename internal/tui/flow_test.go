package tui

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/labstack/driga/internal/flowtest"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
	"github.com/labstack/driga/internal/viewer"
)

func TestRenderedFramesNeverContainTabs(t *testing.T) {
	for _, color := range []bool{false, true} {
		m := setup(t, fixture(), Options{}).(*app)
		m.opts.NoColor = !color
		m.flow = flowFixture()
		m.flow.Mixer.Rows[0].Name = "registry\t"
		if strings.ContainsRune(m.flowPanel("+18 quiet threads", []string{"Enter / click to expand\t\t\t"}, 60, false), '\t') {
			t.Error("quiet panel emits literal tabs")
		}
		for i := 0; i < 18; i++ {
			m.flow.Children = append(m.flow.Children, view.FlowNode{ID: int64(100 + i), Kind: "hosted", Label: "idle", Title: "quiet worker"})
		}
		for _, width := range []int{80, 120, 160} {
			for tab := 0; tab <= flowTab; tab++ {
				t.Run(fmt.Sprintf("color=%t/width=%d/tab=%d", color, width, tab), func(t *testing.T) {
					m.tab = tab
					m.Update(tea.WindowSizeMsg{Width: width, Height: 60})
					m.rebuild()
					if text := m.View().Content; strings.ContainsRune(text, '\t') {
						t.Errorf("frame contains literal tab: %q", text)
					}
					if tab == flowTab && strings.ContainsRune(m.flowView(), '\t') {
						t.Error("flow layout contains literal tabs")
					}
					// Details can include tabwriter output and retained text.
					m.detail = true
					m.viewport.SetContent("Enter / click to expand\t\t\t│")
					if strings.ContainsRune(m.View().Content, '\t') {
						t.Error("detail frame contains literal tabs")
					}
					m.detail = false
				})
			}
		}
	}
}

func flowFixture() view.FlowView {
	now := flowtest.Now
	mixer := model.StateMixer{ObservedAt: now, Verdict: "working", Evidence: "stored verdict", Rows: []model.StateSignal{{Name: "registry", Reading: "busy"}, {Name: "hooks", Reading: "Stop"}, {Name: "telemetry", Reading: "1 / min"}, {Name: "process", Reading: "alive"}}}
	lead := view.FlowNode{ID: 1, Kind: "lead", MachineID: "local", Adapter: "claude", ExternalID: "lead", Title: "Lead", Label: "working", Mixer: mixer, StartedAt: now.Add(-time.Hour), RetainedFromAt: now.Add(-40 * time.Minute), Events: []model.Event{{Kind: "otlp:user_prompt", TS: now.Add(-10 * time.Minute)}}}
	f := view.FlowView{Lead: &lead, Mixer: mixer, ObservedAt: now, WindowMinutes: 60}
	for i := 0; i < 3; i++ {
		id := int64(i + 2)
		parent := int64(1)
		f.Children = append(f.Children, view.FlowNode{ID: id, MachineID: "local", ExternalID: fmt.Sprint("worker-", i), Adapter: "codex", Title: []string{strings.Repeat("worker ", 40), "Hosted", "Finished"}[i], Label: "working", Mixer: mixer, ParentID: &parent, StartedAt: now.Add(time.Duration(i-50) * time.Minute), Events: []model.Event{{Kind: "retained", TS: now.Add(-time.Minute)}}, Jump: &model.InboxJump{Kind: "resume", Command: "codex resume 'fixture'"}})
	}
	return f
}
func TestFlowWidthsAndPulse(t *testing.T) {
	now := time.Now()
	m := setup(t, fixture(), Options{Now: func() time.Time { return now }}).(*app)
	m.tab = flowTab
	m.opts.NoColor = false
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
		for _, want := range []string{"Lead", "signal mixer", "registry", "hooks", "telemetry", "process", "this session family"} {
			if !strings.Contains(text, want) {
				t.Errorf("%d missing %s", width, want)
			}
		}
		if strings.Contains(text, "──●") {
			t.Fatal("pulse without event")
		}
	}
	m.width = 160
	m.Update(flowEventMsg{view.TailEvent{Key: "new", MachineID: "other", Adapter: "codex", ExternalID: "worker-0"}})
	if strings.Contains(m.flowView(), "◉") {
		t.Fatal("pulse from another machine")
	}
	m.Update(flowEventMsg{view.TailEvent{Key: "new", MachineID: "local", Adapter: "codex", ExternalID: "worker-0"}})
	if !strings.Contains(m.flowView(), "◉") {
		t.Fatal("missing event pulse")
	}
	now = now.Add(2 * time.Second)
	if strings.Contains(m.flowView(), "◉") {
		t.Fatal("pulse did not expire")
	}
}
func TestFlowWorkerKeysAndMouse(t *testing.T) {
	calls := 0
	m := setup(t, fixture(), Options{Exec: func(args []string) error {
		calls++
		requireFixtureTerminal(t, args)
		return nil
	}}).(*app)
	m.tab = flowTab
	m.flow = flowFixture()
	m.flow.Children[0].Jump = fixtureLocalJump(t, *m.flow.Children[0].Jump)
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
	if len(hits) != 4 {
		t.Fatal(hits)
	}
	h := hits[1]
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: h.x, Y: h.y + lipgloss.Height(m.header()) + lipgloss.Height(m.tabBar()) + 1})
	if m.cursors[flowTab] != 1 {
		t.Fatal("click did not select worker")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		cmd()
	}
	wantCalls := 0
	if runtime.GOOS == "windows" {
		wantCalls = 1
	}
	if calls != wantCalls || m.detail || !strings.Contains(m.status, "codex resume") {
		t.Fatal("Enter did not plan selected session jump", m.status)
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
	if m.flowWorkers().Nodes[m.cursors[flowTab]].ID != 2 {
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

func TestFlowEntryUsesDefaultAndSessionKeys(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	m.tab = 0
	m.rows = []tabRow{{agent: 42, cells: []string{"", "working", "selected"}}}
	m.cursors[0] = 0
	m.Update(tea.KeyPressMsg{Code: '7', Text: "7"})
	if m.tab != flowTab || m.flowID != 0 {
		t.Fatal("plain 7 must request the shared default, not the highlighted row")
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

func TestFlowSharedDefault(t *testing.T) {
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	var want int64
	if err := st.Tx(context.Background(), func(tx *store.Tx) (err error) { want, err = flowtest.SeedDefault(tx); return err }); err != nil {
		t.Fatal(err)
	}
	src := viewer.NewStoreSource(st, t.TempDir(), view.Filter{})
	src.Now = func() time.Time { return flowtest.Now }
	m := New(src, Options{DataDir: t.TempDir(), NoColor: true, Now: src.Now}).(*app)
	execute(t, m, m.Init())
	m.tab = 0
	m.rebuild()
	_, cmd := press(m, '7')
	execute(t, m, cmd)
	if m.flow.Lead == nil || m.flow.Lead.ID != want || m.flowID != 0 {
		t.Fatalf("default lead = %+v, requested id %d; want %d via id 0", m.flow.Lead, m.flowID, want)
	}
	// Refresh must keep requesting a default, rather than pinning the first lead.
	execute(t, m, m.loadFlow())
	if m.flowID != 0 {
		t.Fatal("default became an explicit selection")
	}
	// Explicit session cycling remains pinned until plain 7 is pressed again.
	m.flowSession(1)
	explicit := m.flowID
	execute(t, m, m.loadFlow())
	if explicit == 0 || m.flowID != explicit {
		t.Fatal("explicit selection lost")
	}
	_, cmd = press(m, '7')
	execute(t, m, cmd)
	if m.flowID != 0 || m.flow.Lead == nil || m.flow.Lead.ID != want {
		t.Fatal("plain 7 did not restore the default")
	}
}

func TestFlowMapRowsAndParent(t *testing.T) {
	m := setup(t, fixture(), Options{Now: func() time.Time { return flowtest.Now }}).(*app)
	m.flow = flowFixture()
	m.flowMinutes = 60
	for _, width := range []int{40, 80, 100, 140} {
		m.width = width
		text, hits := m.flowLayout()
		if len(hits) != 4 {
			t.Fatal(hits)
		}
		if width >= 100 && !strings.Contains(text, "╰") {
			t.Fatal("missing parent start mark")
		}
		for _, line := range strings.Split(text, "\n") {
			if lipgloss.Width(line) > width {
				t.Fatal(width, line)
			}
		}
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

func TestFlowSelectedMixerAndWidths(t *testing.T) {
	m := setup(t, fixture(), Options{NoColor: true, Now: func() time.Time { return flowtest.Now }}).(*app)
	m.flow = flowFixture()
	m.tab = flowTab
	m.cursors[flowTab] = 1
	m.flow.Children[0].Mixer.Evidence = "worker own stored reason"
	for _, width := range []int{140, 100, 80, 59, 40, 20, 8} {
		m.width = width
		text := m.flowView()
		if width >= 60 && !strings.Contains(text, "worker own stored reason") {
			t.Fatal("wrong mixer")
		}
		for _, line := range strings.Split(text, "\n") {
			if lipgloss.Width(line) > width {
				t.Fatal(width, line)
			}
		}
	}
}
