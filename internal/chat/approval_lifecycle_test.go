package chat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/store"
)

func TestReloadRetiresPendingActions(t *testing.T) {
	b, _, st := fixture(t, true)
	ctx := context.Background()
	row, err := b.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a late failed shutdown save, including a legacy row without Tool.
	for _, tool := range []string{"Bash", ""} {
		m := store.ChatMessage{ID: "pending-" + tool, ConversationID: row.ID, Role: "action", Tool: tool, Text: "Kill target · pending", Status: "pending", CreatedAt: time.Now()}
		if err := st.SaveChatMessage(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	// Keep a failing version from hanging fixture cleanup on its nil channel.
	t.Cleanup(func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		for _, c := range b.conversations {
			for _, a := range c.actions {
				if a.answer == nil {
					a.Status = "denied"
				}
			}
		}
	})
	s, err := b.Read(ctx, row.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range s.Messages {
		if m.Status != "denied" || strings.Contains(m.Text, "pending") || !strings.Contains(m.Text, "interrupted") {
			t.Fatal("stale restored card", m)
		}
	}
	if len(s.Actions) != 1 || s.Actions[0].Status != "denied" {
		t.Fatal(s.Actions)
	}
	if err := b.Decide(ctx, row.ID, "pending-Bash", true); err == nil {
		t.Fatal("restored action was actionable")
	}
	stored, err := st.ChatMessages(row.ID)
	if err != nil || len(stored) != 2 || stored[0].Status != "denied" || stored[1].Status != "denied" {
		t.Fatal(stored, err)
	}
}

func TestResolveWithoutAnswerChannel(t *testing.T) {
	b, _, _ := fixture(t, true)
	row, err := b.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c := &conversation{row: row}
	a := &pendingAction{Action: Action{ID: "orphaned", Tool: "Bash", Status: "pending"}}
	done := make(chan error, 1)
	go func() { done <- b.resolve(c, a, true, "you", "") }()
	select {
	case err := <-done:
		if err != nil || a.Status != "denied" {
			t.Fatal(a, err)
		}
	case <-time.After(time.Second):
		t.Fatal("resolve blocked on absent waiter")
	}
}

func activeApproval(t *testing.T, b *Broker, a *fakeAgent, scope Context) Approval {
	t.Helper()
	c, err := b.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Send(context.Background(), c.ID, "question", scope); err != nil {
		t.Fatal(err)
	}
	r := <-a.starts
	return Approval{Conversation: c.ID, Token: r.Token, Tool: "Bash", Input: json.RawMessage(`{"command":"touch example"}`), ToolUseID: "call"}
}
func pendingCard(t *testing.T, b *Broker, id string) Action {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s, err := b.Read(context.Background(), id, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range s.Actions {
			if a.Status == "pending" {
				return a
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no pending card")
	return Action{}
}
func TestApprovalCloseDeniesCard(t *testing.T) {
	b, a, _ := fixture(t, false)
	in := activeApproval(t, b, a, Context{})
	answer := make(chan Permission, 1)
	go func() {
		p, err := b.Approve(context.Background(), in)
		if err != nil {
			t.Error(err)
		}
		answer <- p
	}()
	card := pendingCard(t, b, in.Conversation)
	b.Close()
	select {
	case p := <-answer:
		if p.Behavior != "deny" {
			t.Fatal(p)
		}
	case <-time.After(time.Second):
		t.Fatal("approval did not terminate")
	}
	if err := b.Decide(context.Background(), in.Conversation, card.ID, true); err == nil {
		t.Fatal("closed card was decided")
	}
}
