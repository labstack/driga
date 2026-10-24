package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/labstack/driga/internal/cleanup"
)

type tabHit struct{ x, y, width, tab int }

func (m *app) mouseClick(msg tea.MouseClickMsg) tea.Cmd {
	if msg.Button != tea.MouseLeft || m.confirm != nil || m.filtering || msg.X < 0 || msg.X >= m.width {
		return nil
	}
	_, hits := m.tabLayout()
	for _, h := range hits {
		if msg.Y == lipgloss.Height(m.header())+h.y && msg.X >= h.x && msg.X < h.x+h.width {
			return m.selectTab(byte('0' + mapTabNumber(h.tab)))
		}
	}
	if m.tab == chatTab {
		return nil
	}
	if m.help || m.detail || !m.loaded {
		return nil
	}
	if m.tab == flowTab {
		base := lipgloss.Height(m.header()) + lipgloss.Height(m.tabBar()) + 1
		y := msg.Y - base + min(m.flowOffset, max(0, lipgloss.Height(m.flowView())-m.bodyHeight()))
		for _, h := range m.flowWorkerHits() {
			if msg.X >= h.x && msg.X < h.x+h.width && y >= h.y && y < h.y+h.height {
				if h.index < 0 {
					m.flowAttention = true
					m.flowAskCursor = -h.index - 1
					m.armBoardPrompt()
				} else {
					m.flowAttention = false
					m.boardCursor = h.index
					m.boardTouched = true
				}
				return nil
			}
		}
		return nil
	}
	row, ok := m.mouseRow(msg.X, msg.Y)
	if !ok {
		return nil
	}
	if m.cursors[m.tab] == row {
		return m.openSelected(true)
	}
	m.cursors[m.tab] = row
	m.rebuild()
	return nil
}

func (m *app) mouseRow(x, y int) (int, bool) {
	base := lipgloss.Height(m.header()) + lipgloss.Height(m.tabBar()) + 1
	y -= base
	if y < 0 || y >= m.bodyHeight() {
		return 0, false
	}
	if m.splitDetail() && x >= m.width/2 {
		return 0, false
	}
	// Locate the cursor in the actual viewport. Bubbles keeps its scroll offset
	// private; using our selection marker also tracks resize and page movement.
	for line, text := range strings.Split(ansi.Strip(m.table.View()), "\n") {
		if strings.HasPrefix(strings.TrimSpace(text), "›") {
			row := m.cursors[m.tab] + y - line
			return row, y > 0 && y < lipgloss.Height(m.table.View()) && row >= 0 && row < len(m.rows)
		}
	}
	return 0, false
}

func (m *app) mouseWheel(msg tea.MouseWheelMsg) tea.Cmd {
	if m.confirm != nil || m.filtering || !m.loaded {
		return nil
	}
	base := lipgloss.Height(m.header()) + lipgloss.Height(m.tabBar()) + 1
	if msg.X < 0 || msg.X >= m.width || msg.Y < base || msg.Y >= base+m.bodyHeight() {
		return nil
	}
	delta := 0
	switch msg.Button {
	case tea.MouseWheelUp:
		delta = -1
	case tea.MouseWheelDown:
		delta = 1
	default:
		return nil
	}
	if m.help {
		m.scrollHelp(delta)
		return nil
	}
	if m.tab == chatTab {
		m.scrollChat(-delta, false)
		return nil
	}
	if m.detail {
		m.viewport.SetYOffset(max(0, m.viewport.YOffset()+delta))
		return nil
	}
	if m.tab == flowTab {
		m.flowOffset = max(0, m.flowOffset+delta)
		return nil
	}
	key := tea.KeyDown
	if delta < 0 {
		key = tea.KeyUp
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: key})
	return cmd
}

// openSelected shares detail and measurement behavior between Enter and clicks.
func (m *app) openSelected(mouse bool) tea.Cmd {
	if m.splitDetail() && !mouse {
		return nil
	}
	r, ok := m.selected()
	if !ok {
		return nil
	}
	if r.inbox != nil && r.inbox.Jump != nil {
		return m.jumpInbox(r)
	}
	m.openDetail(r)
	if r.cleanup == nil || m.busy {
		return nil
	}
	m.busy = true
	c, ctx, now := *r.cleanup, m.opts.Context, m.opts.Now()
	return func() tea.Msg {
		c, err := cleanup.Measure(ctx, c, now)
		return cleanupSizeMsg{c, err}
	}
}
