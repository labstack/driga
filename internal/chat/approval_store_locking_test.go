package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/labstack/driga/internal/store"
)

func TestApprovalLookupDoesNotHoldBrokerLock(t *testing.T) {
	for _, tool := range []string{"mcp__driga__jump", "mcp__driga__ack_inbox", "mcp__driga__kill"} {
		t.Run(tool, func(t *testing.T) {
			b, r := actionTurn(t)
			// Block only the collected-state connection, leaving chat history writable.
			state, err := store.Open("", store.Memory)
			if err != nil {
				t.Fatal(err)
			}
			defer state.Close()
			b.opts.ContextStore = state
			locked, release, released := make(chan struct{}), make(chan struct{}), make(chan error, 1)
			go func() {
				released <- state.Tx(context.Background(), func(*store.Tx) error { close(locked); <-release; return nil })
			}()
			<-locked
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() {
				defer close(done)
				b.Approve(ctx, Approval{Conversation: r.Conversation, Token: r.Token, Tool: tool, Input: json.RawMessage(`{"id":"target","session_id":1}`)})
			}()
			// Give the approval goroutine time to reach the blocked store lookup.
			time.Sleep(30 * time.Millisecond)
			read := make(chan error, 1)
			go func() { _, err := b.Read(ctx, r.Conversation, 0); read <- err }()
			var readErr error
			select {
			case readErr = <-read:
			case <-time.After(300 * time.Millisecond):
				readErr = fmt.Errorf("approval store lookup held broker lock")
			}
			close(release)
			if err := <-released; err != nil {
				t.Fatal(err)
			}
			cancel()
			<-done
			if readErr != nil {
				t.Fatal(readErr)
			}
		})
	}
}
