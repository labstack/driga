package chat

import (
	"context"
	"encoding/json"
	"strings"
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

type bigPanelAgent struct{}

func (bigPanelAgent) Run(_ context.Context, _ Request, emit func(Delta) error) error {
	card, _ := json.Marshal(map[string]any{"tool": "show_panels", "input": map[string]any{"dashboard": map[string]any{"name": "Memory", "description": strings.Repeat("a long description ", 40), "panels": []any{}}}})
	return emit(Delta{Panel: card})
}

// A conversation opened again after a restart keeps its cards whole under
// the snippets policy too; a snippet would cut a card into broken JSON.
func TestPanelCardsSurviveReloadUnderSnippets(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	dir := t.TempDir()
	b, err := New(ctx, st, bigPanelAgent{}, Options{DataDir: dir, Snippets: true})
	if err != nil {
		t.Fatal(err)
	}
	c, err := b.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Send(ctx, c.ID, "show memory", Context{}); err != nil {
		t.Fatal(err)
	}
	settled(t, b, c.ID)
	b.Close()
	again, err := New(ctx, st, bigPanelAgent{}, Options{DataDir: dir, Snippets: true})
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	state, err := again.Read(ctx, c.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range state.Messages {
		if m.Role == "panel" && !json.Valid([]byte(m.Text)) {
			t.Fatalf("card after reload is not JSON: %q", m.Text)
		}
	}
}
