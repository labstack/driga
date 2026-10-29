package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

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

type burstPromptClient struct {
	promptClient
	members []string
	burst   string
}

func (c *burstPromptClient) PromptBursts(context.Context) ([]chat.PromptBurst, error) {
	return nil, nil
}
func (c *burstPromptClient) DecideBurst(_ context.Context, id string, members []string, allow bool) error {
	c.burst = id
	c.members = append([]string(nil), members...)
	return nil
}
func TestPromptBurstTUI(t *testing.T) {
	now := time.Now()
	client := &burstPromptClient{}
	m := setup(t, fixture(), Options{Chat: client, NoColor: true, Now: func() time.Time { return now }}).(*app)
	m.width, m.height, m.tab = 80, 24, 2
	g := chat.PromptBurst{ID: "burst", Session: "session", Cwd: "/repo", OpenedAt: now}
	for i := 1; i <= 9; i++ {
		p := chat.Prompt{ID: fmt.Sprintf("p%d", i), Session: "session", Cwd: "/repo", Tool: "Bash", Input: json.RawMessage(fmt.Sprintf(`{"command":"echo item-%d"}`, i))}
		g.Members = append(g.Members, chat.PromptBurstMember{ID: p.ID, InputHash: fmt.Sprint(i), Prompt: p})
		m.prompts = append(m.prompts, p)
	}
	m.bursts = []chat.PromptBurst{g}
	m.openDetail(tabRow{burst: &g})
	for _, want := range []string{"Allow all 9", "item-9", "Input hash:", "n deny selected"} {
		if !strings.Contains(m.detailText, want) {
			t.Fatal(want, m.detailText)
		}
	}
	if _, cmd := m.Update(tea.KeyPressMsg{Code: 'y'}); cmd != nil {
		t.Fatal("unarmed burst allowed")
	}
	now = now.Add(600 * time.Millisecond)
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'y'})
	if cmd == nil {
		t.Fatal("no bulk command")
	}
	cmd()
	if client.burst != "burst" || len(client.members) != 9 || client.members[8] != "p9" {
		t.Fatal(client.members)
	}
	m.busy = false
	m.burstIndex = 3
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'n'})
	if cmd == nil {
		t.Fatal("no individual denial")
	}
	cmd()
	if client.id != "p4" || client.allow {
		t.Fatal(client.id, client.allow)
	}
	// The submitted snapshot stays at nine even after a late member is published.
	if len(client.members) != 9 {
		t.Fatal(client.members)
	}
}
