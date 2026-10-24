package tui

import (
	"github.com/labstack/driga/internal/store"
	"strings"
	"testing"
	"time"
)

func TestIdleChatFocusDoesNotScheduleBlink(t *testing.T) {
	input := newChatInput()
	if cmd := input.Focus(); cmd != nil {
		t.Fatal("idle focus scheduled a virtual cursor tick")
	}
	input.SetValue("draft")
	if !strings.Contains(input.View(), "draft") {
		t.Fatal("composer lost draft")
	}
}
func BenchmarkIdleChatFrames(b *testing.B) {
	m := New(fixture(), Options{NoColor: true, Now: time.Now}).(*app)
	m.tab = chatTab
	m.loaded = true
	for i := 0; i < 10; i++ {
		m.chatMessages = append(m.chatMessages, store.ChatMessage{ID: string(rune('a' + i)), Role: "assistant", Status: "complete", Text: strings.Repeat("A **completed answer** with `code` and useful text.\n\n", 20)})
	}
	m.View()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		m.View()
	}
}

func TestIdleChatAvoidsRepeatedMarkdownParsing(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	m.tab = chatTab
	for i := 0; i < 10; i++ {
		m.chatMessages = append(m.chatMessages, store.ChatMessage{ID: string(rune('a' + i)), Role: "assistant", Status: "complete", Text: strings.Repeat("A **completed answer** with `code` and useful text.\n\n", 20)})
	}
	m.View()
	allocations := testing.AllocsPerRun(5, func() { m.View() })
	if allocations > 6000 {
		t.Fatalf("idle frame reparsed completed Markdown: %.0f allocations", allocations)
	}
	t.Logf("idle frame allocations: %.0f", allocations)
	m.chatMessages[9].Text = "changed body"
	if !strings.Contains(m.chatView(), "changed body") {
		t.Fatal("text cache stale")
	}
	m.width = 40
	m.chatView()
	if m.chatBodies["a"].width != 36 {
		t.Fatal("resize cache stale")
	}
	m.chatMessages = m.chatMessages[:1]
	m.chatView()
	if len(m.chatBodies) != 1 {
		t.Fatal("retired bodies retained")
	}
}
