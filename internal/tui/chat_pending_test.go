package tui

import (
	"encoding/json"

	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/store"
)

func pendingFixture(t *testing.T) *app {
	m := setup(t, fixture(), Options{}).(*app)
	m.tab, m.width, m.height = chatTab, 80, 24
	m.chatActions = []chat.Action{{ID: "card", Tool: "Bash", Title: "Bash · run", Directory: "/project", Status: "pending", Input: json.RawMessage(`{"command":"echo \u003e result","description":"Check output"}`)}}
	m.chatMessages = []store.ChatMessage{{ID: "reply", Role: "assistant", Status: "streaming"}, {ID: "card", Role: "action", Status: "pending"}, {ID: "queued", Role: "user", Status: "queued", Text: "later"}}
	return m
}

func TestChatApprovalPinnedAt80x24(t *testing.T) {
	m := pendingFixture(t)
	m.chatMessages = append([]store.ChatMessage{{Role: "user", Text: strings.Repeat("older content\n", 50)}}, m.chatMessages...)
	m.chatActions = append(m.chatActions, chat.Action{ID: "second", Status: "pending"}, chat.Action{ID: "third", Status: "pending"})
	for _, offset := range []int{0, 20} {
		m.chatOffset = offset
		got := m.View().Content
		for _, want := range []string{"Approve? · Bash", "echo > result", "Check output", "in /project", "+2 more waiting", "y allow · n deny · alt+y/alt+n while typing", "Ask driga anything…"} {
			if !strings.Contains(got, want) {
				t.Fatalf("missing %q at offset %d:\n%s", want, offset, got)
			}
		}
		if strings.Contains(got, `"command"`) {
			t.Fatal("raw input must start collapsed", got)
		}
		for _, line := range strings.Split(got, "\n") {
			if ansi.StringWidth(line) > 80 {
				t.Fatal("overflow", line)
			}
		}
	}
	m.chatKey(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	m.chatOffset = 0
	if !strings.Contains(m.chatView(), `"command"`) {
		t.Fatal("details did not expand")
	}
}

func TestChatApprovalBellElapsedAndNoSpinner(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	for _, quietStyle := range []bool{false, true} {
		m := setup(t, fixture(), Options{Now: func() time.Time { return now }}).(*app)
		m.opts.NoColor, m.opts.MotionOff = quietStyle, quietStyle
		m.tab = chatTab
		a := chat.Action{ID: "card", Tool: "Bash", Status: "pending"}
		state := chatStateMsg{state: chat.State{Reset: true, Active: true, Actions: []chat.Action{a}}}
		_, cmd := m.Update(state)
		if bells := countChatBells(cmd); bells != 1 {
			t.Fatalf("got %d bells", bells)
		}
		now = now.Add(65 * time.Second)
		if got := ansi.Strip(m.chatStatusLine()); got != "waiting for your approval · Bash · 1m 05s" {
			t.Fatal(got)
		}
		if m.chatAnimating || m.animateChat() != nil {
			t.Fatal("approval animated")
		}
		_, cmd = m.Update(state)
		if countChatBells(cmd) != 0 {
			t.Fatal("heartbeat rang again")
		}

	}
}

func TestChatQueuedHintAndNoEmptyAssistant(t *testing.T) {
	m := pendingFixture(t)
	m.height = 45
	got := m.chatView()
	if !strings.Contains(got, "queued · sends after you answer the card") {
		t.Fatal(got)
	}
	if strings.Contains(got, "\nDriga\n") || strings.Contains(got, "…\n") {
		t.Fatal("empty assistant bubble", got)
	}
	m.chatActions = nil
	if got = m.chatView(); strings.Contains(got, "sends after") || !strings.Contains(got, "queued") {
		t.Fatal(got)
	}
	m.chatContext = chat.Context{}
	if m.chatContextLine() != "" {
		t.Fatal("empty context label")
	}
	if strings.Contains(got, "coming") {
		t.Fatal("header promotion")
	}
}

func TestChatFinalAnswerAfterDecidedCard(t *testing.T) {
	m := pendingFixture(t)
	m.chatActions[0].Status = "denied"
	m.chatActions[0].DecidedBy = "you"
	m.chatActions[0].Result = "Denied"
	m.chatMessages = []store.ChatMessage{{ID: "reply", Role: "assistant", Status: "complete", Text: strings.Repeat("intro\n\n", 20) + "AFTER-ACTION-ANSWER"}, {ID: "card", Role: "action", Status: "denied"}}
	got := m.chatView()
	if !strings.Contains(got, "AFTER-ACTION-ANSWER") {
		t.Fatal(got)
	}
	m.height = 60
	got = m.chatView()
	if strings.Index(got, "denied · you · Denied") > strings.Index(got, "AFTER-ACTION-ANSWER") {
		t.Fatal("card obscures answer", got)
	}
	if strings.Contains(got, "AFTER-ACTION-ANSWER\n\n│") {
		t.Fatal("blank before composer", got)
	}
	m.height = 24
	m.scrollChat(1, true)
	if m.chatOffset == 0 {
		t.Fatal("could not scroll up")
	}
	offset := m.chatOffset
	m.Update(chatStateMsg{state: chat.State{Messages: m.chatMessages, Actions: m.chatActions, Reset: true}})
	if m.chatOffset == 0 || offset == 0 {
		t.Fatal("poll reset reading position")
	}
}

func TestChatCodeLongLineRetained(t *testing.T) {
	code := "START-LONG-CODE" + strings.Repeat("x", 400) + "END-LONG-CODE"
	for _, width := range []int{24, 60, 80, 200} {
		got := chatMarkdown("```\n"+code+"\n```", width, true)
		var recovered strings.Builder
		for _, line := range strings.Split(got, "\n") {
			if ansi.StringWidth(line) > width {
				t.Fatal("overflow")
			}
			recovered.WriteString(strings.TrimPrefix(line, "│ "))
		}
		if recovered.String() != code {
			t.Fatalf("width %d lost code: %s", width, got)
		}
	}
}

func toString(msg tea.Msg) string {
	if raw, ok := msg.(tea.RawMsg); ok {
		if text, ok := raw.Msg.(string); ok {
			return text
		}
	}
	return ""
}

func countChatBells(cmd tea.Cmd) int {
	if cmd == nil {
		return 0
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		count := 0
		for _, c := range batch {
			count += countChatBells(c)
		}
		return count
	}
	if toString(msg) == "\a" {
		return 1
	}
	return 0
}

func TestChatApprovalReadableEditWriteAndDriga(t *testing.T) {
	m := pendingFixture(t)
	for _, tc := range []struct{ tool, input, want string }{
		{"Edit", `{"file_path":"/a","old_string":"é","new_string":"new"}`, "/a · 2 → 3 bytes"},
		{"Write", `{"file_path":"/b","content":"界"}`, "/b · 3 bytes"},
		{"mcp__driga__kill", `{}`, "Kill session #12"},
	} {
		got := m.chatCard(chat.Action{Status: "pending", Tool: tc.tool, Input: json.RawMessage(tc.input), Title: "Kill session #12"})
		if !strings.Contains(got, tc.want) {
			t.Fatal(got)
		}
	}
}

func TestChatRawDetailsRemainScrollable(t *testing.T) {
	m := pendingFixture(t)
	m.chatActions[0].Input = json.RawMessage(`{"command":"echo", "start": "START-RAW", "padding": "` + strings.Repeat("x", 1200) + `", "end": "END-RAW"}`)
	m.chatTools = true
	seenStart, seenEnd := false, false
	for i := 0; i < 100; i++ {
		got := m.chatView()
		if !strings.Contains(got, "Approve? · Bash") || !strings.Contains(got, "alt+y/alt+n") {
			t.Fatal("paging lost approval", got)
		}
		seenStart = seenStart || strings.Contains(got, "START-RAW")
		seenEnd = seenEnd || strings.Contains(got, "END-RAW")
		m.scrollChat(1, true)
	}
	if !seenStart || !seenEnd {
		t.Fatal("raw input truncated", seenStart, seenEnd, m.bodyHeight(), m.chatPageHeight, m.chatView())
	}
}

func TestChatScrolledUpAnchorSurvivesCompletion(t *testing.T) {
	m := chatPagingFixture(t)
	m.scrollChat(1, true)
	before := m.chatView()
	top := strings.Split(strings.Split(before, "ctrl+end latest ")[1], "\n")[1]
	messages := append([]store.ChatMessage(nil), m.chatMessages...)
	messages[0].Text = strings.TrimSuffix(messages[0].Text, "\n```") + "\nQA-new-line-1\nQA-new-line-2\n```"
	m.Update(chatStateMsg{state: chat.State{Messages: messages}})
	if !strings.Contains(m.chatView(), top) || m.chatOffset == 0 {
		t.Fatal("completion moved reading position", m.chatView())
	}
}

func TestChatMessageSpacingAndHeader(t *testing.T) {
	m := pendingFixture(t)
	m.height = 40
	m.chatActions = nil
	m.chatMessages = []store.ChatMessage{{Role: "user", Text: "question\n\n"}, {Role: "assistant", Text: "answer\n\n", Status: "complete"}}
	got := m.chatView()
	if !strings.Contains(got, "question\n\nDriga · complete\nanswer\n│ Ask driga anything…") {
		t.Fatal("message or composer spacing", got)
	}
	if strings.Contains(got, "Context") || strings.Contains(got, "coming") {
		t.Fatal("empty context or promotion", got)
	}
}
