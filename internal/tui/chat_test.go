package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/store"
)

type tuiAgent struct{}

func (tuiAgent) Run(ctx context.Context, r chat.Request, emit func(chat.Delta) error) error {
	return emit(chat.Delta{Text: "Only driga read tools are available."})
}
func TestChatSelectionAndAsync(t *testing.T) {
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	b, err := chat.New(context.Background(), st, tuiAgent{}, chat.Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	m := setup(t, fixture(), Options{Chat: b}).(*app)
	m = key(m, '1').(*app)
	model, cmd := press(m, 'c')
	if model.(*app).tab != chatTab || cmd == nil {
		t.Fatal("Chat did not open asynchronously")
	}
	m = execute(t, model, cmd).(*app)
	if m.chatContext.SessionID != 1 || m.chatID == "" {
		t.Fatal(m.chatContext, m.chatID)
	}
	m.chatInput.SetValue("line one\nline two")
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("send missing")
	}
	m = execute(t, m, cmd).(*app)
	if m.chatInput.Value() != "" {
		t.Fatal("composer not cleared")
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("esc did not Stop")
	}
	execute(t, m, cmd)
}
func TestChatGoldens(t *testing.T) {
	for _, width := range []int{80, 140} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := setup(t, fixture(), Options{}).(*app)
			m.tab = chatTab
			m.width = width
			m.height = 38
			m.chatEphemeral = true
			m.chatContext = chat.Context{Page: "flow", SessionID: 12, Machine: "local"}
			m.chatMessages = []store.ChatMessage{{ID: "one", Role: "user", Text: "What needs me?", Status: "sent"}, {ID: "two", Role: "tool", Text: "mcp__driga__list_inbox", Status: "used"}, {ID: "three", Role: "assistant", Text: "Session #12 needs input. Its last captured state is waiting.\nUse the session view to inspect the evidence.", Status: "complete"}}
			got := m.chatView() + "\n"
			if strings.Contains(got, "\x1b") {
				t.Fatal("NO_COLOR emitted ANSI")
			}
			for _, line := range strings.Split(got, "\n") {
				if lipgloss.Width(line) > width {
					t.Fatalf("overflow: %q", line)
				}
			}
			path := filepath.Join("testdata", fmt.Sprintf("chat-%d.golden", width))
			if os.Getenv("UPDATE_CHAT_GOLDEN") == "1" {
				if err := os.WriteFile(path, []byte(got), 0644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if got != strings.ReplaceAll(string(want), "\r\n", "\n") {
				t.Fatalf("golden drift: %s", path)
			}
		})
	}
}

func TestChatReadableContext(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	m.tab, m.width = chatTab, 140
	m.chatContext = chat.Context{Page: "agents", SessionID: 1, Project: "/Users/example/Projects/example/sample-app", Machine: "eba4b8d2deadbeef"}
	got := m.chatView()
	for _, want := range []string{"[agents]", "[session #1 · test title]", "[project · sample-app]"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, "/Users/") || strings.Contains(got, "eba4b8d2") {
		t.Fatal("raw context identifier:", got)
	}
}

func TestChatSingleTurnStatus(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	m.tab, m.busy = chatTab, true // A background snapshot load is unrelated to Chat.
	for _, active := range []bool{false, true} {
		m.Update(chatStateMsg{state: chat.State{Active: active}})
		got := m.View().Content
		want := "Ready"
		if active {
			want = "Claude is answering"
		}
		if !strings.Contains(got, want) || strings.Contains(got, "working…") || (active && strings.Contains(got, "Ready")) {
			t.Fatalf("contradictory status: %s", got)
		}
	}
}

func TestChatTUIBackendErrorVisible(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	m.tab = chatTab
	message := "driga MCP server did not connect; refusing Chat"
	m.Update(chatStateMsg{state: chat.State{Messages: []store.ChatMessage{{ID: "failed", Role: "assistant", Text: message, Status: "error"}}}})
	got := m.View().Content
	if !strings.Contains(got, message) || strings.Contains(got, "Claude is answering") {
		t.Fatalf("backend error was hidden or still shown as answering: %s", got)
	}
}

type partialTUIAgent struct {
	emitted chan struct{}
	release chan struct{}
}

func (a partialTUIAgent) Run(ctx context.Context, _ chat.Request, emit func(chat.Delta) error) error {
	for _, text := range []string{"First ", "second"} {
		if err := emit(chat.Delta{Text: text}); err != nil {
			return err
		}
		a.emitted <- struct{}{}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-a.release:
		}
	}
	return nil
}
func TestChatTUIRendersPartialsBeforeResult(t *testing.T) {
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := partialTUIAgent{make(chan struct{}, 2), make(chan struct{}, 2)}
	b, err := chat.New(context.Background(), st, a, chat.Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	m := setup(t, fixture(), Options{Chat: b}).(*app)
	m = execute(t, m, m.openChat()).(*app)
	if err := b.Send(context.Background(), m.chatID, "question", chat.Context{}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"First", "First second"} {
		select {
		case <-a.emitted:
		case <-time.After(3 * time.Second):
			t.Fatal("partial did not arrive")
		}
		m = execute(t, m, m.readChat()).(*app)
		got := m.View().Content
		if !m.chatActive || !strings.Contains(got, want) || !strings.Contains(got, "Claude is answering") || strings.Contains(got, "Ready") {
			t.Fatal(got)
		}
		a.release <- struct{}{}
	}
}
