package tui

import (
	"context"
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestAuditStopCancellationThroughGlobalNavigation(t *testing.T) {
	for _, key := range []rune{tea.KeyEscape, '1', 'c', '?', '/'} {
		t.Run(string(key), func(t *testing.T) {
			m := boardApp(t, "busy")
			calls := 0
			m.opts.SessionAction = func(context.Context, string, int64) error { calls++; return nil }
			for m.boardSelected().State == "finished" || !m.boardSelected().Local {
				m.Update(tea.KeyPressMsg{Code: 'j'})
			}
			m.Update(tea.KeyPressMsg{Code: 'K'})
			if m.boardStop == 0 {
				t.Fatal("stop was not armed")
			}
			m.Update(tea.KeyPressMsg{Code: key})
			if m.boardStop != 0 {
				t.Fatalf("key %q left destructive confirmation armed for session %d", key, m.boardStop)
			}
			m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
			_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			if cmd != nil {
				execute(t, m, cmd)
			}
			if calls != 0 {
				t.Fatal("navigation stopped a session")
			}
		})
	}
}

func TestAuditMouseNavigationCancelsStop(t *testing.T) {
	for _, msg := range []tea.Msg{
		tea.MouseClickMsg{Button: tea.MouseLeft, X: 1, Y: 0},
		tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: 1, Y: 10},
	} {
		t.Run(fmt.Sprintf("%T", msg), func(t *testing.T) {
			m := boardApp(t, "busy")
			m.opts.SessionAction = func(context.Context, string, int64) error { t.Fatal("unexpected stop"); return nil }
			for m.boardSelected().State == "finished" || !m.boardSelected().Local {
				m.Update(tea.KeyPressMsg{Code: 'j'})
			}
			m.Update(tea.KeyPressMsg{Code: 'K'})
			if m.boardStop == 0 {
				t.Fatal("stop was not armed")
			}
			m.Update(msg)
			if m.boardStop != 0 {
				t.Fatal("mouse interaction left hidden confirmation armed")
			}
		})
	}
}

func TestAuditTableVimUpNavigation(t *testing.T) {
	m := setup(t, fleetSource(), Options{}).(*app)
	m.Update(tea.KeyPressMsg{Code: '2'})
	if len(m.rows) < 2 {
		t.Fatal("fixture needs two machine rows")
	}
	m.Update(tea.KeyPressMsg{Code: 'j'})
	if m.cursors[m.tab] != 1 {
		t.Fatalf("j selected %d, want 1", m.cursors[m.tab])
	}
	m.Update(tea.KeyPressMsg{Code: 'k'})
	if m.cursors[m.tab] != 0 {
		t.Fatalf("k selected %d, want 0", m.cursors[m.tab])
	}
}
