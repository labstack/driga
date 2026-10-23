package tui

import (
	"strings"
	"testing"
)

func TestChatFooterSaysStopDuringReply(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	m.openChat()
	m.chatActive = true
	if got := m.footer(); !strings.Contains(got, "esc stop") || strings.Contains(got, "esc navigate") {
		t.Fatal(got)
	}
}
