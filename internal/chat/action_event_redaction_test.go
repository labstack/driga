package chat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestActionInputsOnlyOnResetOrActionEvent(t *testing.T) {
	b, r := actionTurn(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan Permission, 1)
	input, _ := json.Marshal(map[string]string{"file_path": "file", "content": strings.Repeat("large-input", 8000)})
	go func() {
		p, _ := b.Approve(ctx, Approval{Conversation: r.Conversation, Token: r.Token, Tool: "Write", Input: input})
		done <- p
	}()
	card := waitAction(t, b, r.Conversation)
	snapshot, err := b.Read(ctx, r.Conversation, 0)
	if err != nil || !snapshot.Reset || len(snapshot.Actions) != 1 || string(snapshot.Actions[0].Input) != string(input) {
		t.Fatal("missing full reset", err)
	}
	b.mu.Lock()
	c := b.conversations[r.Conversation]
	b.appendEvent(c, Event{Kind: "delta", MessageID: "reply", Text: "next token"})
	b.mu.Unlock()
	delta, err := b.Wait(ctx, r.Conversation, snapshot.Cursor)
	if err != nil || delta.Reset || len(delta.Actions) != 0 || len(delta.Events) != 1 || delta.Events[0].Kind != "delta" {
		t.Fatal("delta resent action state", err)
	}
	if err := b.Decide(ctx, r.Conversation, card.ID, false); err != nil {
		t.Fatal(err)
	}
	<-done
	update, err := b.Read(ctx, r.Conversation, delta.Cursor)
	if err != nil || update.Reset || len(update.Actions) != 0 || len(update.Events) != 1 || update.Events[0].Action == nil || update.Events[0].Action.Status != "denied" {
		t.Fatal("action event missing", update, err)
	}
	replay, err := b.Read(ctx, r.Conversation, update.Cursor+1)
	if err != nil || !replay.Reset || len(replay.Actions) != 1 || replay.Actions[0].Status != "denied" {
		t.Fatal("reset did not restore updated card", err)
	}
}
