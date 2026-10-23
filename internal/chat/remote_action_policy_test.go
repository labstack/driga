package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestRemoteSessionActionsRefused(t *testing.T) {
	for _, tool := range []string{"kill", "jump"} {
		for _, mode := range []Mode{Confirm, Auto, FullAuto} {
			t.Run(tool+"/"+string(mode), func(t *testing.T) {
				b, r := actionTurn(t)
				// The context store can be separate from the ephemeral chat history.
				state, err := store.Open("", store.Memory)
				if err != nil {
					t.Fatal(err)
				}
				defer state.Close()
				b.opts.ContextStore = state
				var local, remote int64
				if err := state.Tx(context.Background(), func(tx *store.Tx) error {
					var err error
					local, err = tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "local"}})
					if err != nil {
						return err
					}
					remote, err = tx.ForMachine("peer").UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "remote"}})
					return err
				}); err != nil {
					t.Fatal(err)
				}
				in := Approval{Conversation: r.Conversation, Token: r.Token, Tool: "mcp__driga__" + tool, Input: json.RawMessage(fmt.Sprintf(`{"session_id":%d,"machine":"local"}`, remote))}
				if err := b.SetMode(context.Background(), r.Conversation, mode); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
				defer cancel()
				p, err := b.Approve(ctx, in)
				if err != nil || p.Behavior != "deny" || !strings.Contains(p.Message, "another machine") {
					t.Fatal(p, err)
				}
				in.Input = json.RawMessage(fmt.Sprintf(`{"session_id":%d}`, local))
				if b.remoteTarget(in) {
					t.Fatal("local session refused")
				}
			})
		}
	}
}
