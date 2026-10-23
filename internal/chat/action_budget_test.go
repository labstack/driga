package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestCompletedActionsDoNotExhaustBudget(t *testing.T) {
	b, _, st := fixture(t, true)
	ctx := context.Background()
	row, err := b.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	c, err := b.load(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	c.cancel = func() {}
	c.token = "turn"
	b.mu.Unlock()
	for i := 0; i < MaxMessages+8; i++ {
		done := make(chan error, 1)
		go func() {
			p, err := b.Approve(ctx, Approval{Conversation: row.ID, Token: "turn", Tool: "Bash", Input: json.RawMessage(`{}`)})
			if err == nil && p.Behavior != "deny" {
				err = fmt.Errorf("unexpected permission: %s", p.Behavior)
			}
			done <- err
		}()
		// Wait for the event instead of sleeping through 256 cards.
		deadline := time.After(3 * time.Second)
		for {
			s, err := b.Read(ctx, row.ID, 0)
			if err != nil {
				t.Fatal(err)
			}
			var pending string
			for _, a := range s.Actions {
				if a.Status == "pending" {
					pending = a.ID
				}
			}
			if pending != "" {
				if err := b.Decide(ctx, row.ID, pending, false); err != nil {
					t.Fatal(err)
				}
				break
			}
			select {
			case err := <-done:
				t.Fatalf("action %d rejected: %v", i, err)
			case <-deadline:
				t.Fatal("no card")
			default:
				time.Sleep(time.Millisecond)
			}
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	s, _ := b.Read(ctx, row.ID, 0)
	if len(s.Actions) > MaxMessages {
		t.Fatal(len(s.Actions))
	}
	rows, err := st.ChatMessages(row.ID)
	if err != nil || len(rows) == 0 {
		t.Fatal(err)
	}
	if err := b.Stop(ctx, row.ID); err != nil {
		t.Fatal(err)
	}
}

func TestActionByteBudgetPreservesOutstanding(t *testing.T) {
	b, _, _ := fixture(t, true)
	ctx := context.Background()
	row, _ := b.Create(ctx)
	b.mu.Lock()
	c, err := b.load(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	c.cancel = func() {}
	c.token = "turn"
	c.remote = true
	pending := &pendingAction{Action: Action{ID: "pending", Status: "pending", Input: json.RawMessage(`{}`)}, answer: make(chan Permission, 1)}
	claimed := &pendingAction{Action: Action{ID: "claimed", Status: "allowed", Input: json.RawMessage(`{}`)}, claimed: true}
	c.actions = []*pendingAction{pending, claimed}
	b.mu.Unlock()
	raw, _ := json.Marshal(map[string]string{"command": strings.Repeat("x", 100000)})
	for i := 0; i < 16; i++ {
		if _, err := b.Approve(ctx, Approval{Conversation: row.ID, Token: "turn", Tool: "Bash", Input: raw}); err != nil {
			t.Fatal(i, err)
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if c.actions[0] != pending || c.actions[1] != claimed {
		t.Fatal("outstanding actions evicted")
	}
	if len(c.actions) >= 18 {
		t.Fatal("completed bytes never trimmed")
	}
}
