package tui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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
func TestInboxPromptKeys(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(map[bool]string{false: "deny", true: "allow"}[allow], func(t *testing.T) {
			p := chat.Prompt{ID: "one", Session: "session", Tool: "Bash", Input: json.RawMessage(`{"command":"exact command"}`)}
			c := &promptClient{prompts: []chat.Prompt{p}}
			m := setup(t, fixture(), Options{Chat: c}).(*app)
			m.tab = 2
			msg := m.poll()().(refreshMsg)
			m.Update(msg)
			if len(m.rows) < 2 || m.rows[0].prompt == nil || m.rows[1].inbox == nil {
				t.Fatal("prompt must sit above existing wait", m.rows)
			}
			if !strings.Contains(strings.Join(m.rows[0].cells, " "), "exact command") {
				t.Fatal(m.rows[0])
			}
			if !strings.Contains(m.footer(), "y allow / n deny") {
				t.Fatal(m.footer())
			}
			m.openDetail(m.rows[0])
			if !strings.Contains(m.viewport.View(), "exact command") {
				t.Fatal(m.viewport.View())
			}
			key := 'n'
			if allow {
				key = 'y'
			}
			_, cmd := m.Update(tea.KeyPressMsg{Code: key})
			if cmd == nil {
				t.Fatal("missing answer command")
			}
			msg2 := cmd()
			if batch, ok := msg2.(tea.BatchMsg); ok {
				for _, cmd := range batch {
					if cmd != nil {
						cmd()
					}
				}
			}
			if c.id != "one" || c.allow != allow {
				t.Fatal(c)
			}
		})
	}
}
func TestPromptRefreshWithoutDatabaseChange(t *testing.T) {
	c := &promptClient{}
	m := setup(t, fixture(), Options{Chat: c}).(*app)
	m.tab = 2
	m.loaded = true
	m.version = 1
	c.prompts = []chat.Prompt{{ID: "arrived", Session: "s", Tool: "Write", Input: json.RawMessage(`{"file_path":"f","content":"abc"}`)}}
	msg := m.poll()().(refreshMsg)
	if msg.changed {
		t.Fatal("fixture should not change")
	}
	m.Update(msg)
	if len(m.rows) == 0 || m.rows[0].prompt == nil || m.rows[0].prompt.Detail() != "f · 3 bytes" {
		t.Fatal(m.rows)
	}
	m.openDetail(m.rows[0])
	c.prompts = nil
	m.Update(m.poll()())
	if m.detail || m.detailRow != nil {
		t.Fatal("retired prompt detail retained")
	}
	for _, r := range m.rows {
		if r.prompt != nil {
			t.Fatal("retired prompt retained")
		}
	}
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
