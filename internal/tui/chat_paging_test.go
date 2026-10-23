package tui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/labstack/driga/internal/store"
)

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
