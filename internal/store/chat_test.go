package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInterruptChatLabelsPendingActions(t *testing.T) {
	s := openTest(t, filepath.Join(t.TempDir(), "driga.db"), ReadWrite)
	ctx := context.Background()
	if err := s.SaveConversation(ctx, Conversation{ID: "chat", Agent: "claude", CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"pending", "allowed", "denied"} {
		if err := s.SaveChatMessage(ctx, ChatMessage{ID: status, ConversationID: "chat", Role: "action", Tool: "Bash", Text: "Bash · " + status, Status: status, CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 { // Recovery is idempotent.
		if err := s.InterruptChat(ctx); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.ChatMessages("chat")
	if err != nil || len(rows) != 3 {
		t.Fatal(rows, err)
	}
	for _, m := range rows {
		want := "denied"
		if m.ID == "allowed" {
			want = "failed"
		}
		if m.Status != want || strings.Contains(m.Text, "pending") || strings.Contains(m.Text, "allowed") {
			t.Fatal("stale action label", m)
		}
		count := 1
		if m.ID == "denied" {
			count = 0
		}
		if strings.Count(m.Text, " · interrupted") != count {
			t.Fatal("missing or repeated interrupted label", m)
		}
	}
}
