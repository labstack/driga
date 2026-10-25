package chat

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/labstack/driga/internal/store"
)

type panelAgent struct{}

func (panelAgent) Run(_ context.Context, _ Request, emit func(Delta) error) error {
	card, _ := json.Marshal(map[string]any{"tool": "show_panels", "input": map[string]any{"dashboard": map[string]any{"name": "Memory", "panels": []any{}}}})
	if err := emit(Delta{Panel: card}); err != nil {
		return err
	}
	return emit(Delta{Text: "private answer"})
}

// Under the titles policy a turn's text is not kept, but its panel cards
// are: they hold a panel spec, which the card runs again for live data.
func TestPanelCardsOutliveTheCapturePolicy(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	b, err := New(ctx, st, panelAgent{}, Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	c, err := b.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Send(ctx, c.ID, "show memory", Context{}); err != nil {
		t.Fatal(err)
	}
	settled(t, b, c.ID)
	rows, err := st.ChatMessages(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	var card, answer bool
	for _, m := range rows {
		switch m.Role {
		case "panel":
			card = json.Valid([]byte(m.Text)) && m.Text != ""
		case "assistant":
			answer = m.Text == "" || m.Text == "\n"
		}
	}
	if !card || !answer {
		t.Fatalf("card kept %v, answer dropped %v: %+v", card, answer, rows)
	}
}
