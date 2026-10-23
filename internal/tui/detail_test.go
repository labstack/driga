package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
)

func TestAgentDetailTruncatesLongValues(t *testing.T) {
	s := fixture()
	d := s.snap.AgentDetails[1]
	d.Session.Cwd = "/tmp/" + strings.Repeat("界", 200)
	d.Session.Evidence = strings.Repeat("evidence ", 100)
	d.Procs = []view.ProcRow{
		{Proc: model.Proc{PID: 30, PPID: 1, Cmdline: "node -e first\n\t\x00second " + strings.Repeat("script\n", 850)}},
		{Proc: model.Proc{PID: 123456, PPID: 30, Cmdline: "worker --short", Argv: []string{"worker", "--short"}}},
	}
	s.snap.AgentDetails[1] = d
	m := setup(t, s, Options{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 50})
	m = key(m, tea.KeyEnter)
	text := m.(*app).detailText
	for _, line := range strings.Split(text, "\n") {
		if lipgloss.Width(line) > 120 {
			t.Fatalf("line exceeds width: %d", lipgloss.Width(line))
		}
	}
	if !strings.Contains(text, "Cwd: /tmp/") || !strings.Contains(text, "Evidence: evidence") {
		t.Fatal("expected single-column key/value layout", text)
	}
	var commandColumn int
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "COMMAND") {
			commandColumn = strings.Index(line, "COMMAND")
		}
		if strings.Contains(line, "node -e") {
			if strings.Index(line, "node -e") != commandColumn || !strings.HasSuffix(line, "…") || !strings.Contains(line, "first second") {
				t.Fatal("command not aligned, collapsed and truncated", line)
			}
		}
		if strings.Contains(line, "worker --short") && strings.Index(line, "worker --short") != commandColumn {
			t.Fatal("command columns differ", line)
		}
	}
	if commandColumn == 0 || strings.Contains(text, strings.Repeat("script", 100)) || strings.Count(text, "script") > 20 {
		t.Fatal("command was not truncated")
	}
	for _, width := range []int{80, 120} {
		m, _ = m.Update(tea.WindowSizeMsg{Width: width, Height: 50})
		for _, line := range strings.Split(m.(*app).viewport.View(), "\n") {
			if lipgloss.Width(line) > width {
				t.Fatalf("resized line exceeds %d columns", width)
			}
		}
	}
}
