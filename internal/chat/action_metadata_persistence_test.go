package chat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestActionMetadataSurvivesRedactionAndRestart(t *testing.T) {
	b, _, st := fixture(t, false)
	ctx := context.Background()
	row, _ := b.Create(ctx)
	b.mu.Lock()
	c, err := b.load(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	a := &pendingAction{Action: Action{ID: "audit", Tool: "Bash", Title: "private command", Status: "denied", DecidedBy: "you", Input: json.RawMessage(`{}`)}}
	err = b.actionEvent(c, a)
	b.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	b.Close()
	next, err := New(ctx, st, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	state, err := next.Read(ctx, row.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Actions) != 1 {
		t.Fatal(state)
	}
	got := state.Actions[0]
	if got.Tool != "Bash" || got.Status != "denied" || got.DecidedBy != "you" || strings.Contains(got.Title, "private") {
		t.Fatal(got)
	}
	if state.Messages[0].Text != "" || state.Messages[0].Tool != "Bash" || state.Messages[0].DecidedBy != "you" {
		t.Fatal(state.Messages)
	}
}
