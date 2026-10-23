package tui

import (
	"context"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"errors"
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
	// Shift+Enter and Alt+Enter add lines; plain Enter sends.
	m.chatInput.SetValue("line one")
	for _, mod := range []tea.KeyMod{tea.ModShift, tea.ModAlt} {
		if _, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: mod}); cmd != nil && m.chatInput.Value() == "" {
			t.Fatal("modified Enter sent the draft")
		}
	}
	if got := m.chatInput.Value(); strings.Count(got, "\n") != 2 {
		t.Fatalf("new lines not inserted: %q", got)
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("send missing")
	}
	m = execute(t, m, cmd).(*app)
	if m.chatInput.Value() != "" {
		t.Fatal("composer not cleared")
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.chatActive && cmd == nil {
		t.Fatal("active esc did not Stop")
	}
	if !m.chatActive && m.chatInput.Focused() {
		t.Fatal("idle esc did not blur")
	}
	if cmd != nil {
		execute(t, m, cmd)
	}
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
			m.chatMessages = append(m.chatMessages, store.ChatMessage{ID: "card", Role: "action", Status: "pending", Text: "Bash · touch demo"})
			m.chatActions = []chat.Action{{ID: "card", Tool: "Bash", Title: "Bash · touch demo", Input: []byte(`{"command":"touch demo"}`), Directory: "/project", Status: "pending"}}
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
			want = "thinking…"
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
	if !strings.Contains(got, message) || strings.Contains(got, "thinking…") {
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
		if !m.chatActive || !strings.Contains(got, want) || !strings.Contains(got, "writing…") || strings.Contains(got, "Ready") {
			t.Fatal(got)
		}
		a.release <- struct{}{}
	}
}

func TestChatFocusNavigation(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	m.openChat()
	m.chatInput.SetValue("draft text")
	key(m, 'q')
	if m.chatInput.Value() != "draft textq" {
		t.Fatal("typing routed to navigation")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.chatInput.Focused() {
		t.Fatal("idle Escape must blur")
	}
	if !strings.Contains(m.footer(), "c type · m trust mode · N tabs · ? help · q quit") {
		t.Fatal(m.footer())
	}
	key(m, '?')
	if !m.help {
		t.Fatal("navigate help unreachable")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.help {
		t.Fatal("help did not close")
	}
	key(m, '/')
	if !m.filtering {
		t.Fatal("navigate filter unreachable")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.chatInput.Focused() {
		t.Fatal("Enter must type")
	}
	if !strings.Contains(m.footer(), "esc navigate · enter send · shift+enter new line · alt+N tabs") {
		t.Fatal(m.footer())
	}
	for _, typing := range []bool{true, false} {
		for n := '0'; n <= '8'; n++ {
			m.openChat()
			if !typing {
				m.chatInput.Blur()
			}
			m.Update(tea.KeyPressMsg{Code: n, Mod: tea.ModAlt})
			want := int(n - '1')
			if n == '0' {
				want = homeTab
			}
			if n == '7' {
				want = flowTab
			}
			if n == '8' {
				want = chatTab
			}
			if m.tab != want {
				t.Fatalf("alt+%c typing=%v: tab %d", n, typing, m.tab)
			}
			if m.chatInput.Value() != "draft textq" {
				t.Fatal("tab switch lost draft")
			}
		}
	}
	m.openChat()
	m.chatInput.Blur()
	key(m, '1')
	if m.tab != 0 {
		t.Fatal("navigate tab unreachable")
	}
	key(m, '8')
	if m.chatInput.Value() != "draft textq" || !m.chatInput.Focused() {
		t.Fatal("return did not restore draft")
	}
	m.chatInput.Blur()
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q'})
	if cmd == nil {
		t.Fatal("navigate quit unreachable")
	}
	m.openChat()
	m.chatInput.Blur()
	key(m, 'c')
	if !m.chatInput.Focused() {
		t.Fatal("c must type")
	}
}

func TestChatActivityStates(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	m := setup(t, fixture(), Options{Now: func() time.Time { return now }}).(*app)
	m.tab, m.width, m.height = chatTab, 100, 40
	row := store.ChatMessage{ID: "reply", Role: "assistant", Status: "streaming"}
	_, cmd := m.Update(chatStateMsg{state: chat.State{Active: true, Messages: []store.ChatMessage{row}}})
	if cmd == nil {
		t.Fatal("active reply must poll")
	}
	now = now.Add(65 * time.Second)
	got := m.View().Content
	for _, want := range []string{"thinking…", "1m 05s", "Driga", "answered by Claude Code"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, "\x1b") || !strings.Contains(got, "… thinking…") {
		t.Fatal("NoColor spinner", got)
	}
	row.Text = "Hello"
	m.Update(chatStateMsg{state: chat.State{Active: true, Messages: []store.ChatMessage{row}, Events: []chat.Event{{Seq: 1, Kind: "delta", MessageID: row.ID, Text: "Hello"}}}})
	got = m.View().Content
	if !strings.Contains(got, "writing…") || !strings.Contains(got, "Hello▍") {
		t.Fatal(got)
	}
	tool := store.ChatMessage{Role: "tool", Text: "mcp__driga__list_agents"}
	m.Update(chatStateMsg{state: chat.State{Active: true, Messages: []store.ChatMessage{row, tool}, Events: []chat.Event{{Seq: 2, Kind: "message", Message: &tool}}}})
	if got = m.View().Content; !strings.Contains(got, "reading agents…") {
		t.Fatal(got)
	}
	m.Update(chatStateMsg{state: chat.State{Active: true, Messages: []store.ChatMessage{row, tool}, Events: []chat.Event{{Seq: 3, Kind: "delta", MessageID: row.ID, Text: "more"}}}})
	if got = m.View().Content; !strings.Contains(got, "writing…") {
		t.Fatal(got)
	}
	row.Status = "complete"
	_, cmd = m.Update(chatStateMsg{state: chat.State{Messages: []store.ChatMessage{row}}})
	if cmd != nil {
		t.Fatal("idle reply must not schedule ticks")
	}
	if got = m.View().Content; strings.Contains(got, "writing…") || strings.Contains(got, "▍") {
		t.Fatal(got)
	}
	_, cmd = m.Update(chatTickMsg{})
	if cmd != nil {
		t.Fatal("late tick scheduled idle work")
	}
}

func TestChatEscapeStopsBeforeBlurring(t *testing.T) {
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
	if err := b.Send(context.Background(), m.chatID, "test", chat.Context{}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-a.emitted:
	case <-time.After(3 * time.Second):
		t.Fatal("no reply")
	}
	m = execute(t, m, m.readChat()).(*app)
	m.chatInput.SetValue("keep me")
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil || !m.chatInput.Focused() {
		t.Fatal("active Escape must stop before blurring")
	}
	execute(t, m, cmd)
	deadline := time.Now().Add(3 * time.Second)
	for m.chatActive && time.Now().Before(deadline) {
		m = execute(t, m, m.readChat()).(*app)
		if m.chatActive {
			time.Sleep(time.Millisecond)
		}
	}
	if m.chatActive {
		t.Fatal("reply did not stop")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.chatInput.Focused() || m.chatInput.Value() != "keep me" {
		t.Fatal("second Escape must blur and retain draft")
	}
}

func TestChatElapsed(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{-time.Second, "0s"}, {0, "0s"}, {999 * time.Millisecond, "0s"}, {59 * time.Second, "59s"}, {60 * time.Second, "1m 00s"}, {65 * time.Second, "1m 05s"}, {time.Hour, "60m 00s"},
	} {
		if got := chatElapsed(tc.d); got != tc.want {
			t.Errorf("%v: %q, want %q", tc.d, got, tc.want)
		}
	}
}

type toolTUIAgent struct{ emitted, release chan struct{} }

func (a toolTUIAgent) Run(ctx context.Context, _ chat.Request, emit func(chat.Delta) error) error {
	for _, delta := range []chat.Delta{{Tool: "mcp__driga__list_agents"}, {Text: "Found agents"}} {
		if err := emit(delta); err != nil {
			return err
		}
		a.emitted <- struct{}{}
		select {
		case <-a.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
func TestChatActivityUsesTransportEvents(t *testing.T) {
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := toolTUIAgent{make(chan struct{}, 2), make(chan struct{}, 2)}
	b, err := chat.New(context.Background(), st, a, chat.Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	m := setup(t, fixture(), Options{Chat: b}).(*app)
	m = execute(t, m, m.openChat()).(*app)
	if err := b.Send(context.Background(), m.chatID, "test", chat.Context{}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"reading agents…", "writing…"} {
		select {
		case <-a.emitted:
		case <-time.After(3 * time.Second):
			t.Fatal("no event")
		}
		m = execute(t, m, m.readChat()).(*app)
		if got := m.chatActivity(); !strings.Contains(got, want) {
			t.Fatalf("want %q: %s", want, got)
		}
		// Colour is applied after cleaning: no escape remnants on screen.
		frame := ansi.Strip(m.View().Content)
		if strings.Contains(frame, "[38;") || strings.Contains(frame, "[m") || strings.Count(frame, want) != 1 {
			t.Fatalf("activity rendered badly:\n%s", frame)
		}
		a.release <- struct{}{}
	}
}

func TestChatSpinnerLifecycle(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	m.opts.NoColor = false
	m.tab = chatTab
	_, cmd := m.Update(chatStateMsg{state: chat.State{Active: true}})
	if cmd == nil || !m.chatAnimating {
		t.Fatal("spinner did not start")
	}
	before := m.chatSpinner.View()
	_, cmd = m.Update(m.chatSpinner.Tick())
	if cmd == nil || m.chatSpinner.View() == before {
		t.Fatal("spinner did not animate")
	}
	m.Update(chatStateMsg{err: errors.New("Claude Code unavailable")})
	_, cmd = m.Update(spinner.TickMsg{ID: m.chatSpinner.ID()})
	if cmd != nil || m.chatActive || m.chatAnimating {
		t.Fatal("error left spinner running")
	}
	if !strings.Contains(m.chatActivity(), "Claude Code unavailable") {
		t.Fatal(m.chatActivity())
	}
	m.opts.NoColor = true
	m.Update(chatStateMsg{state: chat.State{Active: true}})
	if m.chatAnimating {
		t.Fatal("NoColor started animation")
	}
	_, cmd = m.Update(m.chatSpinner.Tick())
	if cmd != nil {
		t.Fatal("NoColor tick scheduled animation")
	}
	m.opts.NoColor = false
	m.opts.MotionOff = true
	if cmd = m.animateChat(); cmd != nil {
		t.Fatal("MotionOff animated")
	}
}

func TestChatConversationSwitchInvalidatesTicks(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	m.tab = chatTab
	m.chatInput.Focus()
	m.chatActive = true
	m.chatPolling = true
	m.chatID = "old"
	m.chatHistory = []store.Conversation{{ID: "old"}, {ID: "other"}}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})
	if cmd == nil || m.chatID != "other" || m.chatActive {
		t.Fatal("history did not restart read")
	}
	_, cmd = m.Update(chatTickMsg{generation: 0})
	if cmd != nil || !m.chatPolling {
		t.Fatal("stale tick changed new poll")
	}
	_, cmd = m.Update(chatStateMsg{generation: 0, state: chat.State{Active: true}})
	if cmd != nil || m.chatActive {
		t.Fatal("stale state revived reply")
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	if cmd == nil || m.chatID != "" {
		t.Fatal("new did not restart read")
	}
}

func TestChatCursorAfterMarkdown(t *testing.T) {
	for _, text := range []string{"---", "```go\nfmt.Println(1)\n```", "# Heading", "**Hello**"} {
		m := setup(t, fixture(), Options{}).(*app)
		m.tab = chatTab
		m.width = 80
		m.height = 40
		m.Update(chatStateMsg{state: chat.State{Active: true, Messages: []store.ChatMessage{{ID: "r", Role: "assistant", Status: "streaming", Text: text}}}})
		if got := m.chatView(); !strings.Contains(got, "▍") {
			t.Fatal(got)
		}
	}
}

func TestChatNavigateControlsAndAltOverlay(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	m.openChat()
	m.chatInput.Blur()
	key(m, '?')
	m.Update(tea.KeyPressMsg{Code: '1', Mod: tea.ModAlt})
	if m.help || m.tab != 0 {
		t.Fatal("Alt tabs did not dismiss help")
	}
	m.openChat()
	m.chatInput.Blur()
	m.chatMessages = []store.ChatMessage{{ID: "reply", Role: "user", Text: strings.Repeat("scrollable line\n", 100)}}
	m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	if m.chatOffset == 0 {
		t.Fatal("navigate PgUp did not scroll chat")
	}
	m.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	if !m.chatTools || m.chatInput.Focused() {
		t.Fatal("navigate chat controls")
	}
	m.confirm = &confirmation{}
	m.Update(tea.KeyPressMsg{Code: '8', Mod: tea.ModAlt})
	if m.confirm != nil {
		t.Fatal("Alt tabs kept a stale confirmation")
	}
}

func TestChatReadErrorEndsCursor(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	m.tab = chatTab
	m.height = 40
	m.Update(chatStateMsg{state: chat.State{Active: true, Messages: []store.ChatMessage{{ID: "reply", Role: "assistant", Status: "streaming", Text: "Partial"}}}})
	m.Update(chatStateMsg{err: errors.New("connection failed")})
	if got := m.chatView(); strings.Contains(got, "▍") || strings.Contains(got, "· streaming") {
		t.Fatal(got)
	}
}
