package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"fmt"
	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
	"regexp"
	"strings"
	"testing"
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
		if first != status+" · you" {
			t.Fatal(first)
		}
		for _, line := range strings.Split(got, "\n") {
			if len([]rune(line)) > m.width {
				t.Fatal("unwrapped card")
			}
		}
	}
}
func chatPagingFixture(t *testing.T) *app {
	m := setup(t, fixture(), Options{}).(*app)
	m.tab, m.width, m.height = chatTab, 60, 20
	var lines []string
	for i := 1; i <= 40; i++ {
		lines = append(lines, fmt.Sprintf("QA-line-%02d", i))
	}
	m.chatMessages = []store.ChatMessage{{ID: "reply", Role: "assistant", Status: "complete", Text: "```\n" + strings.Join(lines, "\n") + "\n```"}}
	m.chatInput.Blur()
	return m
}
func TestChatPagesOverlapAtShortHeight(t *testing.T) {
	m := chatPagingFixture(t)
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		for _, line := range regexp.MustCompile(`QA-line-[0-9]+`).FindAllString(m.chatView(), -1) {
			seen[line] = true
		}
		m.chatKey(tea.KeyPressMsg{Code: tea.KeyPgUp})
	}
	for i := 1; i <= 40; i++ {
		if !seen[fmt.Sprintf("QA-line-%02d", i)] {
			t.Fatalf("page skipped line %d", i)
		}
	}
}
func TestChatScrollClampsWithoutDebt(t *testing.T) {
	m := chatPagingFixture(t)
	for i := 0; i < 100; i++ {
		m.chatKey(tea.KeyPressMsg{Code: tea.KeyPgUp})
	}
	before := m.chatView()
	m.chatKey(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if before == m.chatView() {
		t.Fatalf("PgDown did not move: offset %d", m.chatOffset)
	}
	for i := 0; i < 100; i++ {
		m.scrollChat(1, false)
	}
	if m.chatOffset > m.chatMaxOffset {
		t.Fatal("wheel overscroll")
	}
}
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
func TestAppliedFilterVisibleAndEscapeClears(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	key(m, '1')
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

type selectionSource struct {
	*fakeSource
	calls []int64
}

func (s *selectionSource) LoadAgent(_ context.Context, _ store.Scope, id int64) (view.AgentView, error) {
	s.calls = append(s.calls, id)
	return view.AgentView{Session: model.Session{ID: id, Evidence: "selected evidence"}}, nil
}
func (s *selectionSource) LoadProject(context.Context, store.Scope, int64, bool) (view.WhereView, error) {
	return view.WhereView{}, nil
}
func TestTUILoadsOnlySelectedSessionDetail(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	src := &selectionSource{fakeSource: fixture()}
	m.src = src
	m.snap.AgentDetails = nil
	m.tab = 0
	m.rebuild()
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("selection did not request detail")
	}
	execute(t, m, cmd)
	if len(src.calls) != 1 || src.calls[0] != 1 || len(m.snap.AgentDetails) != 1 || !strings.Contains(m.View().Content, "selected evidence") {
		t.Fatal(src.calls, m.View().Content)
	}
	if _, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatal("unchanged selection reloaded detail")
	}
}

func TestChatFooterSaysStopDuringReply(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	m.openChat()
	m.chatActive = true
	if got := m.footer(); !strings.Contains(got, "esc stop") || strings.Contains(got, "esc navigate") {
		t.Fatal(got)
	}
}

func TestEmptyStatesExplainTabAndFilter(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	for tab := 0; tab <= 5; tab++ {
		m.tab = tab
		m.rows = nil
		if got := m.View().Content; strings.Contains(got, "No items") || !strings.Contains(got, "No ") {
			t.Fatal(tab, got)
		}
		m.filter.SetValue("no-match")
		if got := m.View().Content; !strings.Contains(got, "No matching items") {
			t.Fatal(tab, got)
		}
		m.filter.SetValue("")
	}
}

func TestChatPositionAndLatestShortcut(t *testing.T) {
	for _, typing := range []bool{false, true} {
		m := chatPagingFixture(t)
		if typing {
			m.chatInput.Focus()
		}
		m.chatInput.SetValue("unsent draft")
		m.chatKey(tea.KeyPressMsg{Code: tea.KeyPgUp})
		if got := m.View().Content; !strings.Contains(got, "Lines ") || !strings.Contains(got, "ctrl+end latest") {
			t.Fatal(got)
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyEnd, Mod: tea.ModCtrl})
		if got := m.View().Content; m.chatOffset != 0 || strings.Contains(got, "ctrl+end latest") || !strings.Contains(got, "QA-line-40") || m.chatInput.Value() != "unsent draft" {
			t.Fatal(got)
		}
	}
}

func TestHelpAndUserMessagesWrapWords(t *testing.T) {
	if got := wrapText("ordinary words remain together", 15); got != "ordinary words\nremain together" {
		t.Fatal(got)
	}
	if got := wrapText("12345678901234567890", 10); got != "1234567890\n1234567890" {
		t.Fatal("long tokens must still fit", got)
	}
}
