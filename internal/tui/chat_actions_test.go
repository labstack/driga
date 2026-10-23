package tui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/store"
)

type actionClient struct {
	chat.Client
	mode     chat.Mode
	decision string
	allow    bool
}

func TestChatActionsApplyEventUpdates(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	m.tab = chatTab
	row := store.Conversation{ID: "conversation"}
	card := chat.Action{ID: "card", Tool: "Write", Status: "pending", Input: json.RawMessage(`{"file_path":"file","content":"large input"}`)}
	m.Update(chatStateMsg{state: chat.State{Conversation: row, Reset: true, Cursor: 10, Actions: []chat.Action{card}}})
	m.Update(chatStateMsg{state: chat.State{Conversation: row, Cursor: 11, Events: []chat.Event{{Seq: 11, Kind: "delta", MessageID: "reply", Text: "token"}}}})
	if len(m.chatActions) != 1 || m.chatActions[0].Status != "pending" {
		t.Fatal("text delta dropped action", m.chatActions)
	}
	card.Status = "allowed"
	m.Update(chatStateMsg{state: chat.State{Conversation: row, Cursor: 12, Events: []chat.Event{{Seq: 12, Kind: "action", Action: &card}}}})
	if len(m.chatActions) != 1 || m.chatActions[0].Status != "allowed" || len(m.chatActions[0].Input) == 0 {
		t.Fatal("action event did not update card", m.chatActions)
	}
	// A reset replaces the live card window, including clearing obsolete cards.
	m.Update(chatStateMsg{state: chat.State{Conversation: row, Reset: true, Cursor: 20, Actions: []chat.Action{}}})
	if len(m.chatActions) != 0 {
		t.Fatal(m.chatActions)
	}
}

func (c *actionClient) SetMode(_ context.Context, _ string, m chat.Mode) error {
	c.mode = m
	return nil
}
func (c *actionClient) Decide(_ context.Context, _ string, id string, allow bool) error {
	c.decision, c.allow = id, allow
	return nil
}

func TestChatActionKeys(t *testing.T) {
	c := &actionClient{}
	m := setup(t, fixture(), Options{Chat: c}).(*app)
	m.tab, m.chatID = chatTab, "conversation"
	m.chatMode = chat.Confirm
	m.chatActions = []chat.Action{{ID: "oldest", Title: "Bash · touch demo", Status: "pending", Input: json.RawMessage(`{"command":"touch demo"}`)}, {ID: "newest", Status: "pending"}}
	for _, tc := range []struct {
		code          rune
		mod           tea.KeyMod
		typing, allow bool
	}{
		{'y', 0, false, true}, {'n', 0, false, false}, {'y', tea.ModAlt, true, true}, {'n', tea.ModAlt, true, false},
	} {
		if tc.typing {
			m.chatInput.Focus()
		} else {
			m.chatInput.Blur()
		}
		_, cmd := m.Update(tea.KeyPressMsg{Code: tc.code, Mod: tc.mod})
		if cmd == nil {
			t.Fatal("missing decision command")
		}
		cmd()
		if c.decision != "oldest" || c.allow != tc.allow {
			t.Fatal(c)
		}
		if !strings.Contains(m.footer(), "allow/deny") || ansi.StringWidth(m.footer()) > m.width {
			t.Fatal(m.footer())
		}
	}
	m.chatInput.SetValue("")
	m.chatInput.Focus()
	m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	if m.chatInput.Value() != "ynm" {
		t.Fatal("typing consumed action keys", m.chatInput.Value())
	}
	m.chatInput.Blur()
	for _, mode := range []chat.Mode{chat.Auto, chat.FullAuto, chat.Confirm} {
		if mode == chat.FullAuto {
			// The first press only explains; any other key cancels.
			if _, cmd := m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"}); cmd != nil || !strings.Contains(m.chatStatus, "30 minutes") {
				t.Fatal("Full auto needs a confirming press", m.chatStatus)
			}
			m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
			if m.chatFullAsk || c.mode != chat.Auto {
				t.Fatal("other key must cancel Full auto")
			}
			m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
		}
		_, cmd := m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
		if cmd == nil {
			t.Fatal("missing mode command")
		}
		cmd()
		if c.mode != mode {
			t.Fatal(c.mode, mode)
		}
		m.chatMode = mode
	}
	if m.chatActivity() != "action waiting" {
		t.Fatal(m.chatActivity())
	}
}
