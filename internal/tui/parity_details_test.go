package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/labstack/driga/internal/view"
)

func panelDetailsApp(t *testing.T) *app {
	t.Helper()
	s := fixture()
	s.snap.LocalMachineID = "local"
	now := s.snap.ObservedAt
	node := func(id int64, machine, project string) view.FlowNode {
		return view.FlowNode{ID: id, MachineID: machine, Project: project, Title: fmt.Sprintf("session %d on %s", id, machine), Adapter: "claude", Label: "working", StartedAt: now.Add(-time.Hour), LastActivityAt: now}
	}
	s.snap.BoardData = view.BoardSnapshot{Nodes: []view.FlowNode{node(11, "local", "repo"), node(12, "local", "repo"), node(13, "local", "elsewhere"), node(11, "remote", "repo"), node(99, "remote", "repo")}}
	s.snap.SessionPanels = []view.SessionPanel{
		{ID: 11, MachineID: "remote", Turns: []string{"failed"}, Files: []string{"remote-secret.go"}},
		{ID: 11, MachineID: "local", Turns: []string{"ok", "waited", "failed"}, Files: []string{"src/a.go", "src/b.go"}},
		{ID: 12, MachineID: "local", Files: []string{"src/a.go"}},
		{ID: 13, MachineID: "local", Files: []string{"src/a.go"}},
		{ID: 99, MachineID: "remote", Files: []string{"src/a.go"}},
	}
	m := setup(t, s, Options{}).(*app)
	m.tab = flowTab
	m.flowViewMode = "Sessions"
	m.boardTouched = true
	m.width = 140
	m.height = 40
	m.syncBoard()
	for i, r := range m.boardRows() {
		if r.Node.ID == 11 && r.Node.MachineID == "local" {
			m.boardCursor = i
			break
		}
	}
	return m
}

func TestBoardSessionPanelParity(t *testing.T) {
	for _, width := range []int{80, 100, 120, 140, 200} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := panelDetailsApp(t)
			m.width = width
			text, _ := m.boardLayout()
			for _, want := range []string{"3 turns · ✓◆×", "✓ went through · ◆ waited · × failed", "2 files", "src/a.go · also #12", "src/b.go"} {
				if !strings.Contains(text, want) {
					t.Fatalf("missing %q at width %d:\n%s", want, width, text)
				}
			}
			for _, wrong := range []string{"remote-secret.go", "also #13", "also #99"} {
				if strings.Contains(text, wrong) {
					t.Fatalf("wrong session metadata %q:\n%s", wrong, text)
				}
			}
			if lipgloss.Height(text) > m.bodyHeight() {
				t.Fatalf("height overflow: %d > %d", lipgloss.Height(text), m.bodyHeight())
			}
			for _, line := range strings.Split(text, "\n") {
				if lipgloss.Width(line) > width {
					t.Fatalf("width overflow: %q", line)
				}
			}
		})
	}
}

func TestBoardSessionPanelUnknownIsAbsent(t *testing.T) {
	for _, known := range []bool{false, true} {
		m := panelDetailsApp(t)
		m.snap.SessionPanels = nil
		if known {
			m.snap.SessionPanels = []view.SessionPanel{{ID: 11, MachineID: "local"}}
		}
		for _, width := range []int{80, 140} {
			m.width = width
			text, _ := m.boardLayout()
			for _, unknown := range []string{"0 turns", "0 files", "went through", "also #"} {
				if strings.Contains(text, unknown) {
					t.Fatalf("unknown metadata rendered %q:\n%s", unknown, text)
				}
			}
		}
	}
}

func TestBoardSessionPanelLastThirtyTurns(t *testing.T) {
	m := panelDetailsApp(t)
	for i := range m.snap.SessionPanels {
		if m.snap.SessionPanels[i].ID == 11 && m.snap.SessionPanels[i].MachineID == "local" {
			m.snap.SessionPanels[i].Turns = append([]string{"failed", "failed"}, strings.Split(strings.TrimSuffix(strings.Repeat("ok,", 30), ","), ",")...)
		}
	}
	m.width = 80
	m.height = 24
	text, _ := m.boardLayout()
	if !strings.Contains(text, "30 turns · "+strings.Repeat("✓", 30)) || strings.Contains(text, "32 turns") || strings.Contains(text, "××") {
		t.Fatal(text)
	}
	if lipgloss.Height(text) > m.bodyHeight() {
		t.Fatalf("height overflow: %d > %d\n%s", lipgloss.Height(text), m.bodyHeight(), text)
	}
}
