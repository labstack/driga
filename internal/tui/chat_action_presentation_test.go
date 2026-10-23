package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/store"
)

func TestFullAutoWarningSurvivesActivePoll(t *testing.T) {
	client := &actionClient{}
	m := setup(t, fixture(), Options{Chat: client}).(*app)
	m.tab, m.chatID, m.chatMode = chatTab, "conversation", chat.Auto
	m.width, m.height = 80, 24
	m.chatInput.Blur()
	m.chatActive = true
	m.Update(tea.KeyPressMsg{Code: 'm'})
	for _, active := range []bool{true, false, true} {
		m.Update(chatStateMsg{state: chat.State{Mode: chat.Auto, Active: active, Conversation: store.Conversation{ID: "conversation"}}})
		got := m.View().Content
		if !m.chatFullAsk || !strings.Contains(got, "without asking for 30 minutes") || !strings.Contains(got, "m again to confirm") {
			t.Fatal(got)
		}
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'm'})
	if cmd == nil {
		t.Fatal("missing confirming command")
	}
	cmd()
	if client.mode != chat.FullAuto {
		t.Fatal(client.mode)
	}
}
func TestActionHeaderKeepsStatusAndActor(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	m.width = 80
	for _, status := range []string{"pending", "denied", "done"} {
		got := m.chatCard(chat.Action{Title: "Write · " + strings.Repeat("界🙂/", 80), Tool: "Write", Status: status, DecidedBy: "you"})
		first := strings.Split(got, "\n")[0]
		if status == "pending" {
			if !strings.Contains(got, "Approve? · Write") {
				t.Fatal(got)
			}
		} else if !strings.Contains(first, status+" · you") {
			t.Fatal(first)
		}
		for _, line := range strings.Split(got, "\n") {
			if len([]rune(line)) > m.width {
				t.Fatal("unwrapped card")
			}
		}
	}
}
