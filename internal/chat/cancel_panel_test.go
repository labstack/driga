package chat

import (
	"context"
	"encoding/json"
	"github.com/labstack/driga/internal/store"
	"testing"
)

type canceledPanelAgent struct{ started chan struct{} }

func (a canceledPanelAgent) Run(ctx context.Context, _ Request, emit func(Delta) error) error {
	close(a.started)
	<-ctx.Done()
	_ = emit(Delta{Panel: json.RawMessage(`{"tool":"create_dashboard","input":{"dashboard":{"name":"Review"}},"status":"unknown","error":"Tool result unavailable; outcome unknown"}`)})
	return ctx.Err()
}
func TestCanceledTurnRetainsUnknownPanelOutcome(t *testing.T) {
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	started := make(chan struct{})
	b, err := New(context.Background(), st, canceledPanelAgent{started}, Options{DataDir: t.TempDir(), Snippets: true})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	c, err := b.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Send(context.Background(), c.ID, "show", Context{}); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := b.Stop(context.Background(), c.ID); err != nil {
		t.Fatal(err)
	}
	settled(t, b, c.ID)
	state, err := b.Read(context.Background(), c.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range state.Messages {
		if m.Role == "panel" {
			var card struct{ Status string }
			if json.Unmarshal([]byte(m.Text), &card) != nil || card.Status != "unknown" {
				t.Fatal(m.Text)
			}
			return
		}
	}
	t.Fatal("canceled tool outcome lost")
}
