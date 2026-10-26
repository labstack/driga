package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

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

// Panel specs contain conversation text and follow the same capture policy.
func TestPanelCardsFollowTheCapturePolicy(t *testing.T) {
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
			card = m.Text == ""
		case "assistant":
			answer = m.Text == "" || m.Text == "\n"
		}
	}
	if !card || !answer {
		t.Fatalf("card dropped %v, answer dropped %v: %+v", card, answer, rows)
	}
}

type bigPanelAgent struct{}

func (bigPanelAgent) Run(_ context.Context, _ Request, emit func(Delta) error) error {
	card, _ := json.Marshal(map[string]any{
		"tool": "show_panels",
		"input": map[string]any{
			"dashboard": map[string]any{"name": "Memory ghp_private", "description": strings.Repeat("a long description ", 40), "panels": []any{map[string]any{"text": "ghp_panel"}}},
			"vars":      map[string]any{"project": "ghp_variable"},
		},
		"error": "ghp_error",
	})
	return emit(Delta{Panel: card})
}

// Recursive snippets keep card JSON valid while bounding and scrubbing text.
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
	rows, err := st.ChatMessages(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range rows {
		if m.Role == "panel" {
			checkPanelCapture(t, m.Text)
		}
	}
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
		if m.Role == "panel" {
			checkPanelCapture(t, m.Text)
		}
	}
}

func checkPanelCapture(t *testing.T, text string) {
	t.Helper()
	if !json.Valid([]byte(text)) || strings.Contains(text, "ghp_") || !strings.Contains(text, "[redacted]") {
		t.Fatalf("panel was not scrubbed as valid JSON: %q", text)
	}
	var card struct {
		Input struct {
			Dashboard struct{ Description string }
		}
	}
	if err := json.Unmarshal([]byte(text), &card); err != nil {
		t.Fatal(err)
	}
	if len([]rune(card.Input.Dashboard.Description)) != 200 {
		t.Fatalf("panel description was not bounded: %q", card.Input.Dashboard.Description)
	}
}

func TestPanelCaptureAlsoFiltersStoredHistory(t *testing.T) {
	b, _, st := fixture(t, true)
	ctx := context.Background()
	c, err := b.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	private := `{"tool":"show_panels","input":{"vars":{"project":"ghp_stored"}},"error":"ghp_failure"}`
	if err := st.SaveChatMessage(ctx, store.ChatMessage{ID: "old-panel", ConversationID: c.ID, Role: "panel", Text: private, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	state, err := b.Read(ctx, c.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Messages) != 1 || strings.Contains(state.Messages[0].Text, "ghp_") || !json.Valid([]byte(state.Messages[0].Text)) {
		t.Fatalf("stored panel escaped capture policy: %+v", state.Messages)
	}
}

type privateErrorAgent struct{}

func (privateErrorAgent) Run(context.Context, Request, func(Delta) error) error {
	return errors.New("private project /work/customer/acquisition failed")
}

func TestTurnFailureLogsOnlyErrorMetadata(t *testing.T) {
	var log bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&log, nil)))
	defer slog.SetDefault(previous)
	ctx := context.Background()
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	b, err := New(ctx, st, privateErrorAgent{}, Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	c, err := b.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Send(ctx, c.ID, "private question", Context{}); err != nil {
		t.Fatal(err)
	}
	settled(t, b, c.ID)
	if strings.Contains(log.String(), "acquisition") || strings.Contains(log.String(), "private project") || !strings.Contains(log.String(), "error_class") || !strings.Contains(log.String(), "error_bytes") {
		t.Fatalf("error log leaked private text or lost diagnostic metadata: %s", log.String())
	}
}
