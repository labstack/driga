package chat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestApprovalModesRemoteAndAsk(t *testing.T) {
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
