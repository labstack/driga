package tui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/chat"
)

type promptClient struct {
	chat.Client
	prompts []chat.Prompt
	id      string
	allow   bool
}

func (c *promptClient) Prompts(context.Context) ([]chat.Prompt, error) { return c.prompts, nil }
func (c *promptClient) AnswerPrompt(_ context.Context, id string, allow bool) error {
	c.id, c.allow = id, allow
	return nil
}
func TestPromptDetailKeepsFullInput(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	input, _ := json.Marshal(map[string]string{"command": strings.Repeat("long-command ", 80) + "end-marker"})
	p := chat.Prompt{ID: "one", Session: "session", Tool: "Bash", Input: input}
	m.openDetail(tabRow{prompt: &p})
	if !strings.Contains(m.detailText, "end-marker") || strings.Contains(m.detailText, "…") {
		t.Fatal("prompt input truncated", m.detailText)
	}
}

func TestPromptDetailDecodesEscapesAndKeepsRaw(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	raw := json.RawMessage(`{"command":"echo \u754c \u003e result"}`)
	p := chat.Prompt{ID: "one", Session: "s", Tool: "Bash", Input: raw}
	m.openDetail(tabRow{prompt: &p})
	decoded, rawPart, ok := strings.Cut(m.detailText, "Raw JSON")
	if !ok || !strings.Contains(decoded, "echo 界 > result") || strings.Contains(decoded, `\u003e`) || !strings.Contains(rawPart, `\u003e`) {
		t.Fatal(m.detailText)
	}
	if string(p.Input) != string(raw) {
		t.Fatal("raw payload changed")
	}
}
