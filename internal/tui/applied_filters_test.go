package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestAppliedFilterVisibleAndEscapeClears(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	key(key(m, '0'), 's')
	key(m, '/')
	m.filter.SetValue("qa-no-match-界🙂")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := m.View().Content; !strings.Contains(got, "Filter: qa-no-match-界🙂 · esc clear") {
		t.Fatal(got)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.filter.Value() != "" || len(m.rows) == 0 {
		t.Fatal("applied filter did not clear")
	}
}
