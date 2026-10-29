package chat

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestStoppedActionCompletesWithOwnToken(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			b, a, _ := fixture(t, true)
			ctx := context.Background()
			in := activeApproval(t, b, a, Context{})
			in.Tool = "mcp__driga__ack_inbox"
			if err := b.SetMode(ctx, in.Conversation, FullAuto); err != nil {
				t.Fatal(err)
			}
			if p, err := b.Approve(ctx, in); err != nil || p.Behavior != "allow" {
				t.Fatal(p, err)
			}
			id, err := b.Claim(in)
			if err != nil {
				t.Fatal(err)
			}
			if err := b.Stop(ctx, in.Conversation); err != nil {
				t.Fatal(err)
			}
			s := settled(t, b, in.Conversation)
			if len(s.Actions) != 1 || s.Actions[0].Status != "stopped" || s.Actions[0].Result != "result unknown" {
				t.Fatal(s.Actions)
			}
			if _, err := b.Claim(in); err == nil {
				t.Fatal("stopped token claimed new work")
			}
			// Other conversations must not evict a claimed action awaiting its result.
			for range MaxConversations {
				row, err := b.Create(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := b.Read(ctx, row.ID, 0); err != nil {
					t.Fatal(err)
				}
			}
			if err := b.Send(ctx, in.Conversation, "next turn", Context{}); err != nil {
				t.Fatal(err)
			}
			next := <-a.starts
			wrong := in
			wrong.Token = next.Token
			if err := b.Complete(wrong, id, "wrong", false); err == nil {
				t.Fatal("new turn completed old action")
			}
			if err := b.Complete(in, id, "actual result", failed); err != nil {
				t.Fatal(err)
			}
			if err := b.Complete(in, id, "replay", false); err == nil {
				t.Fatal("completion replayed")
			}
			s, err = b.Read(ctx, in.Conversation, 0)
			if err != nil {
				t.Fatal(err)
			}
			want := "done"
			if failed {
				want = "failed"
			}
			if s.Actions[0].Status != want || s.Actions[0].Result != "actual result" {
				t.Fatal(s.Actions)
			}
			rows, err := b.st.ChatMessages(in.Conversation)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range rows {
				if m.ID == id && (m.Status != want || !strings.Contains(m.Text, "actual result")) {
					t.Fatal(m)
				}
			}
		})
	}
}
