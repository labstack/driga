package chat

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestChatRemoteCloseEndsFullAuto(t *testing.T) {
	b, _, _ := fixture(t, true)
	dir := t.TempDir()
	stop, err := Listen(context.Background(), dir, b)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	client, err := Dial(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	conv, err := client.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SetMode(context.Background(), conv.ID, FullAuto); err != nil {
		t.Fatal(err)
	}
	client.Close()
	state, err := b.Read(context.Background(), conv.ID, 0)
	if err != nil || state.Mode != Confirm {
		t.Fatal(state.Mode, err)
	}
}

func TestFullAutoExpiresToAuto(t *testing.T) {
	b, a, _ := fixture(t, true)
	b.fullAutoTTL = 50 * time.Millisecond
	in := activeApproval(t, b, a, Context{})
	if err := b.SetMode(context.Background(), in.Conversation, FullAuto); err != nil {
		t.Fatal(err)
	}
	s, _ := b.Read(context.Background(), in.Conversation, 0)
	if s.Mode != FullAuto || s.Until.IsZero() {
		t.Fatal(s.Mode, s.Until)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		s, _ = b.Read(context.Background(), in.Conversation, 0)
		if s.Mode == Auto {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Full auto did not expire:", s.Mode)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !s.Until.IsZero() {
		t.Fatal("Auto must not carry an end time")
	}
	// After expiry a shell command asks again instead of running.
	b.approvalTTL = 50 * time.Millisecond
	in.Tool, in.Input = "Bash", json.RawMessage(`{"command":"touch x"}`)
	if p, err := b.Approve(context.Background(), in); err != nil || p.Behavior != "deny" {
		t.Fatal(p, err)
	}
	s, _ = b.Read(context.Background(), in.Conversation, 0)
	if last := s.Actions[len(s.Actions)-1]; last.Status != "expired" || last.DecidedBy == "full auto" {
		t.Fatal(last)
	}
}
func TestFullAutoEvictionDisposesTimer(t *testing.T) {
	b, _, _ := fixture(t, true)
	ctx := context.Background()
	row, _ := b.Create(ctx)
	if err := b.SetMode(ctx, row.ID, FullAuto); err != nil {
		t.Fatal(err)
	}
	old := b.conversations[row.ID]
	timer := old.expire
	for i := 0; i < MaxConversations; i++ {
		r, _ := b.Create(ctx)
		if _, err := b.Read(ctx, r.ID, 0); err != nil {
			t.Fatal(err)
		}
	}
	if b.conversations[row.ID] != nil {
		t.Fatal("not evicted")
	}
	b.Close()
	if timer.Stop() || old.expire != nil || old.mode != Confirm || len(old.events) != 0 {
		t.Fatal("evicted state retained its timer/buffers")
	}
}
