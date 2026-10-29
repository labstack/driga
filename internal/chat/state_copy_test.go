package chat

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func TestReadReturnsIndependentActionPayloads(t *testing.T) {
	b, _, _ := fixture(t, true)
	ctx := context.Background()
	row, err := b.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	const raw = `{"file_path":"plain.txt"}`
	b.mu.Lock()
	c, err := b.load(row.ID)
	if err != nil {
		b.mu.Unlock()
		t.Fatal(err)
	}
	before := c.seq
	action := &pendingAction{Action: Action{ID: "one", Tool: "Write", Title: "Write plain.txt", Status: "denied", Input: json.RawMessage(raw)}}
	c.actions = append(c.actions, action)
	err = b.actionEvent(c, action)
	b.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := b.Read(ctx, row.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Actions[0].Input[0] = '['
	events, err := b.Read(ctx, row.ID, before)
	if err != nil {
		t.Fatal(err)
	}
	if len(events.Events) != 1 {
		t.Fatal(events.Events)
	}
	events.Events[0].Action.Title = "client changed title"
	events.Events[0].Action.Input[0] = ']'
	events.Events[0].Message.Text = "client changed message"
	again, err := b.Read(ctx, row.ID, 0)
	if err != nil || string(again.Actions[0].Input) != raw {
		t.Fatal("snapshot mutated broker payload", again.Actions, err)
	}
	replay, err := b.Read(ctx, row.ID, before)
	if err != nil || replay.Events[0].Action.Title != "Write plain.txt" || replay.Events[0].Message.Text == "client changed message" || string(replay.Events[0].Action.Input) != raw {
		t.Fatal("snapshot mutated replay", replay.Events, err)
	}
}

func TestReadReturnsIndependentPreparedCommands(t *testing.T) {
	b, in, _ := commandTurn(t)
	ctx := context.Background()
	p, err := b.prepareCommand(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := b.approveCommand(ctx, in, p); done <- err }()
	card := waitAction(t, b, in.Conversation)
	state, err := b.Read(ctx, in.Conversation, 0)
	if err != nil {
		t.Fatal(err)
	}
	state.Actions[0].Command.Requested.Argv[0] = "modified"
	state.Actions[0].Command.Effective[0][0] = "modified"
	again, err := b.Read(ctx, in.Conversation, 0)
	if err != nil || !reflect.DeepEqual(*again.Actions[0].Command, PresentCommand(p)) {
		t.Fatal("client mutated prepared command", err)
	}
	events, err := b.Read(ctx, in.Conversation, again.Cursor-1)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events.Events {
		if e.Action != nil {
			e.Action.Command.Requested.Argv[0] = "changed"
		}
	}
	if err = b.Decide(ctx, in.Conversation, card.ID, false); err != nil {
		t.Fatal(err)
	}
	<-done
}
