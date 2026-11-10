package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/store"
)

func TestChatProviderCaptionUsesConversationState(t *testing.T) {
	for _, tt := range []struct{ provider, caption string }{
		{"claude", "answered by Claude Code"},
		{"codex", "answered by Codex"},
		{"copilot", "answered by GitHub Copilot CLI"},
		{"", "assistant not selected"},
		{"unknown", "assistant not selected"},
	} {
		t.Run(tt.provider, func(t *testing.T) {
			m := setup(t, fixture(), Options{}).(*app)
			m.tab, m.width, m.height = chatTab, 100, 38
			m.Update(chatStateMsg{state: chat.State{Conversation: store.Conversation{ID: "conversation", Agent: tt.provider}}})
			got := m.chatView()
			if !strings.Contains(got, tt.caption) {
				t.Fatalf("missing %q in %s", tt.caption, got)
			}
			if tt.provider != "claude" && strings.Contains(got, "answered by Claude Code") {
				t.Fatal("caption attributed another provider to Claude")
			}
		})
	}
}

func TestChatProviderCaptionClearsWhileSwitching(t *testing.T) {
	for _, key := range []rune{'n', 'h'} {
		t.Run(string(key), func(t *testing.T) {
			m := setup(t, fixture(), Options{}).(*app)
			m.tab, m.width, m.height = chatTab, 100, 38
			m.chatID, m.chatProvider = "old", "codex"
			m.chatHistory = []store.Conversation{{ID: "old", Agent: "codex"}, {ID: "other", Agent: "claude"}}
			m.chatKey(tea.KeyPressMsg{Code: key, Mod: tea.ModCtrl})
			if got := m.chatView(); !strings.Contains(got, "assistant not selected") || strings.Contains(got, "answered by Codex") {
				t.Fatalf("stale provider during switch: %s", got)
			}
		})
	}
}
