package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
)

func TestMouseTabClick(t *testing.T) {
	for _, width := range []int{40, 120, 160} {
		m := setup(t, fixture(), Options{}).(*app)
		m.Update(tea.WindowSizeMsg{Width: width, Height: 44})
		for y, line := range strings.Split(ansi.Strip(m.tabBar()), "\n") {
			if x := strings.Index(line, "1 Projects"); x >= 0 {
				m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: x + 2, Y: lipgloss.Height(m.header()) + y})
			}
		}
		if m.tab != 1 {
			t.Errorf("width %d: tab %d", width, m.tab)
		}
		if m.View().MouseMode != tea.MouseModeCellMotion {
			t.Error("cell-motion mouse mode disabled")
		}
	}
}

func mouseFixture() *fakeSource {
	s := fixture()
	s.snap.Agents.Rows = nil
	s.snap.Inbox.Rows = nil
	s.snap.Orphans.Rows = nil
	for i := 1; i <= 3; i++ {
		id := int64(i)
		s.snap.Agents.Rows = append(s.snap.Agents.Rows, view.AgentRow{ID: id, Label: "working", Title: fmt.Sprint("agent", i)})
		s.snap.Inbox.Rows = append(s.snap.Inbox.Rows, view.InboxRow{InboxItem: model.InboxItem{ID: fmt.Sprint("event", i), SessionID: id, Kind: "waiting", TS: s.snap.ObservedAt}})
		s.snap.Projects.Rows = append(s.snap.Projects.Rows, view.ProjectRow{Status: view.ProjectStatus{Repo: model.Repo{ID: id, Name: fmt.Sprint("project", i)}}})
		s.snap.Orphans.Rows = append(s.snap.Orphans.Rows, view.OrphanRow{Orphan: model.Orphan{ID: fmt.Sprint("orphan", i)}})
		s.snap.Cleanup = append(s.snap.Cleanup, cleanup.Candidate{ID: fmt.Sprint("cleanup", i)})
	}
	return s
}
