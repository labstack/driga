package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestHelpScrollsAtShortHeight(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	m.width, m.height = 60, 20
	m.tab = chatTab
	m.chatInput.Blur()
	key(m, '?')
	before := m.View().Content
	m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if before == m.View().Content || m.helpOffset == 0 {
		t.Fatal("help did not page")
	}
	for i := 0; i < 30; i++ {
		m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if !strings.Contains(m.View().Content, "ctrl+c quit") {
		t.Fatal(m.View().Content)
	}
	m.mouseWheel(tea.MouseWheelMsg{Button: tea.MouseWheelUp, X: 10, Y: 8})
	if m.helpOffset == 0 {
		t.Fatal("help unexpectedly jumped to top")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.help {
		t.Fatal("help did not close")
	}
}
