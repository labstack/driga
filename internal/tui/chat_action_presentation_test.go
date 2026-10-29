package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/store"
)

func TestChatModeWordingMatchesApp(t *testing.T) {
	now := time.Now()
	m := setup(t, fixture(), Options{NoColor: true, Now: func() time.Time { return now }}).(*app)
	m.width, m.height = 100, 30
	for mode, label := range map[chat.Mode]string{chat.Confirm: "Confirm", chat.Auto: "Auto", chat.FullAuto: "Full auto · 30m left"} {
		m.chatMode, m.chatUntil = mode, now.Add(30*time.Minute)
		if got := m.chatView(); !strings.Contains(got, " · "+label) {
			t.Fatalf("mode %s: missing %q in %s", mode, label, got)
		}
	}
}

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
		if !m.chatFullAsk || !strings.Contains(got, "automatically for 30 minutes") || !strings.Contains(got, "m again to confirm") {
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

func TestCommandCardTUI(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {140, 50}} {
		m := setup(t, fixture(), Options{NoColor: true}).(*app)
		m.width, m.height = size[0], size[1]
		c := &chat.CommandCard{Summary: "Stop 65 processes owned by you", Boundary: "Host process snapshot only", Class: "Process"}
		for i := 0; i < 65; i++ {
			c.Targets = append(c.Targets, chat.ProcessRow{PID: int32(100 + i), Owner: "UID 501", Executable: "/bin/java"})
		}
		a := chat.Action{ID: "cmd", Tool: "mcp__driga__run_command", Status: "pending", Input: []byte(`{"argv":["pkill","-f","maestro"]}`), Command: c}
		if got := m.chatCard(a); !strings.Contains(got, "Stop 65") || !strings.Contains(got, "ctrl+t details") {
			t.Fatal(got)
		}
		details := chatActionDetails(a)
		if !strings.Contains(details, `"pid": 164`) || !strings.Contains(details, "UID 501") {
			t.Fatal(details)
		}
	}
}
