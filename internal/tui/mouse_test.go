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
			if x := strings.Index(line, "3 Inbox"); x >= 0 {
				m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: x + 2, Y: lipgloss.Height(m.header()) + y})
			}
		}
		if m.tab != 2 {
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

func TestMouseRowSelectAndOpen(t *testing.T) {
	for _, tab := range []int{homeTab, 0, 1, 2, 3, 5} {
		for _, width := range []int{80, 120, 160} {
			t.Run(fmt.Sprintf("tab%d-width%d", tab, width), func(t *testing.T) {
				m := setup(t, mouseFixture(), Options{}).(*app)
				m.tab = tab
				m.Update(tea.WindowSizeMsg{Width: width, Height: 44})
				bodyY := lipgloss.Height(m.header()) + lipgloss.Height(m.tabBar()) + 1
				y := bodyY + 2 // table header, then the second row
				if tab == homeTab {
					needsW := width
					if width >= 150 {
						needsW = width / 2
					} else if width >= 110 {
						needsW = (width - 1) * 3 / 5
					}
					if needsW < 76 {
						y++
					} // Home locations wrap onto a second line.
				}
				msg := tea.MouseClickMsg{Button: tea.MouseLeft, X: 3, Y: y}
				m.Update(msg)
				if m.cursors[tab] != 1 || m.detail {
					t.Errorf("first click: cursor=%d detail=%v", m.cursors[tab], m.detail)
				}
				m.Update(msg) // Commands are deliberately not run: no scratch measurement IO.
				if !m.detail || m.detailRow == nil || m.detailRow.key != m.rows[1].key {
					t.Errorf("second click did not open selected row")
				}
			})
		}
	}
}

func TestMouseWheelMovesLists(t *testing.T) {
	for _, tab := range []int{homeTab, 0, 1, 2, 3, 5} {
		m := setup(t, mouseFixture(), Options{}).(*app)
		m.tab = tab
		m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		y := lipgloss.Height(m.header()) + lipgloss.Height(m.tabBar()) + 3
		m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: 3, Y: y})
		if m.cursors[tab] != 1 {
			t.Errorf("tab %d wheel down: %d", tab, m.cursors[tab])
		}
		m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp, X: 3, Y: y})
		if m.cursors[tab] != 0 {
			t.Errorf("tab %d wheel up: %d", tab, m.cursors[tab])
		}
	}
}

func TestMouseScrolledTableAndHome(t *testing.T) {
	for _, tab := range []int{0, homeTab} {
		s := mouseFixture()
		for i := 4; i < 80; i++ {
			s.snap.Agents.Rows = append(s.snap.Agents.Rows, view.AgentRow{ID: int64(i), Label: "waiting", Title: fmt.Sprint("agent", i)})
		}
		m := setup(t, s, Options{}).(*app)
		m.tab = tab
		m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
		for i := 0; i < 40; i++ {
			key(m, tea.KeyDown)
		}
		bodyY := lipgloss.Height(m.header()) + lipgloss.Height(m.tabBar()) + 1
		y, want := bodyY+1, m.cursors[tab]-5
		if tab == 0 {
			for i, line := range strings.Split(ansi.Strip(m.table.View()), "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "›") {
					y = bodyY + i - 1
					want = m.cursors[tab] - 1
					break
				}
			}
		} else {
			m.homeOffset = 1 // clicking after Home scroll must account for the clipped frame
			y--
		}
		m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 3, Y: y})
		if m.cursors[tab] != want {
			t.Errorf("tab %d scrolled click: got %d want %d", tab, m.cursors[tab], want)
		}
	}
}

func TestMouseRepeatedClickAfterScroll(t *testing.T) {
	for _, tab := range []int{0, homeTab} {
		s := mouseFixture()
		for i := 4; i < 80; i++ {
			s.snap.Agents.Rows = append(s.snap.Agents.Rows, view.AgentRow{ID: int64(i), Label: "waiting", Title: fmt.Sprint("agent", i)})
		}
		m := setup(t, s, Options{}).(*app)
		m.tab = tab
		m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		for i := 0; i < 40; i++ {
			key(m, tea.KeyDown)
		}
		bodyY := lipgloss.Height(m.header()) + lipgloss.Height(m.tabBar()) + 1
		msg := tea.MouseClickMsg{Button: tea.MouseLeft, X: 3, Y: bodyY + 1}
		m.Update(msg)
		want := m.rows[m.cursors[tab]].key
		m.Update(msg)
		if !m.detail || m.detailRow == nil || m.detailRow.key != want {
			t.Errorf("tab %d: repeated click after scrolling did not open %s", tab, want)
		}
	}
}
