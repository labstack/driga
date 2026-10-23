package chat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

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

func TestPhase2ModesRemoteAndAsk(t *testing.T) {
	for _, mode := range []Mode{Auto, FullAuto} {
		t.Run(string(mode), func(t *testing.T) {
			b, a, _ := fixture(t, true)
			in := activeApproval(t, b, a, Context{})
			if err := b.SetMode(context.Background(), in.Conversation, mode); err != nil {
				t.Fatal(err)
			}
			if mode == Auto {
				in.Tool = "mcp__driga__jump"
				in.Input = json.RawMessage(`{"session_id":1}`)
			}
			p, err := b.Approve(context.Background(), in)
			if err != nil || p.Behavior != "allow" {
				t.Fatal(p, err)
			}
			s, _ := b.Read(context.Background(), in.Conversation, 0)
			if s.Actions[0].DecidedBy != string(mode) {
				t.Fatal(s.Actions)
			}
		})
	}
	b, a, _ := fixture(t, true)
	in := activeApproval(t, b, a, Context{Machine: "remote"})
	if err := b.SetMode(context.Background(), in.Conversation, FullAuto); err != nil {
		t.Fatal(err)
	}
	p, err := b.Approve(context.Background(), in)
	if err != nil || p.Behavior != "deny" || !strings.Contains(p.Message, "another machine") {
		t.Fatal(p, err)
	}
	b2, a2, _ := fixture(t, true)
	ask := activeApproval(t, b2, a2, Context{Page: "ask"})
	if _, err := b2.Approve(context.Background(), ask); err == nil {
		t.Fatal("ask can act")
	}
}
func TestPhase2SocketCapabilities(t *testing.T) {
	b, a, _ := fixture(t, true)
	dir := t.TempDir()
	closeSocket, err := Listen(context.Background(), dir, b)
	if err != nil {
		t.Fatal(err)
	}
	defer closeSocket()
	client, err := Dial(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	in := activeApproval(t, b, a, Context{})
	if err := client.SetMode(context.Background(), in.Conversation, FullAuto); err != nil {
		t.Fatal(err)
	}
	p, err := Bridge(context.Background(), SocketPath(dir), in)
	if err != nil || p.Behavior != "allow" {
		t.Fatal(p, err)
	}
	child, err := dialPath(context.Background(), SocketPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	child.token = in.Token
	if err := child.SetMode(context.Background(), in.Conversation, Confirm); err == nil {
		t.Fatal("turn token changed trust mode")
	}
	if err := child.Decide(context.Background(), in.Conversation, "anything", true); err == nil {
		t.Fatal("turn token decided a card")
	}
	if err := child.call(context.Background(), "POST", "/approve", map[string]any{"conversation": in.Conversation, "token": in.Token, "mode": "confirm", "allow": true}, nil); err == nil {
		t.Fatal("approve accepted client fields")
	}
	s, _ := b.Read(context.Background(), in.Conversation, 0)
	if s.Mode != FullAuto {
		t.Fatal(s.Mode)
	}
}

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
