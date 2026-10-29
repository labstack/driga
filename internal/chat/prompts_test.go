package chat

import (
	"context"
	"encoding/json"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"strings"
	"testing"
	"time"
)

func testPrompt(id string) Prompt {
	return Prompt{ID: id, Session: "session", Cwd: "/repo", Tool: "Bash", Input: json.RawMessage(`{"command":"private prompt input"}`)}
}
func waitPrompt(t *testing.T, b *Broker, id string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		ps, err := b.Prompts(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range ps {
			if p.ID == id {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no prompt", id)
}
func TestPromptAnswerOnce(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(map[bool]string{false: "deny", true: "allow"}[allow], func(t *testing.T) {
			b, _, _ := fixture(t, true)
			ctx := context.Background()
			done := make(chan PromptDecision, 1)
			go func() { p, _ := b.WaitPrompt(ctx, testPrompt("one")); done <- p }()
			waitPrompt(t, b, "one")
			if err := b.AnswerPrompt(ctx, "one", allow); err != nil {
				t.Fatal(err)
			}
			if err := b.AnswerPrompt(ctx, "one", !allow); err == nil || !strings.Contains(err.Error(), "prompt no longer waiting") {
				t.Fatal(err)
			}
			p := <-done
			if (p.Behavior == "allow") != allow || !allow && p.Message != "Denied from driga" {
				t.Fatal(p)
			}
			ps, _ := b.Prompts(ctx)
			if len(ps) != 0 {
				t.Fatal(ps)
			}
		})
	}
}
func TestPromptRetirement(t *testing.T) {
	for _, reason := range []string{"hook exit", "session end", "timeout", "broker close"} {
		t.Run(reason, func(t *testing.T) {
			b, _, _ := fixture(t, true)
			if reason == "timeout" {
				b.promptTTL = 40 * time.Millisecond
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan PromptDecision, 1)
			go func() { p, _ := b.WaitPrompt(ctx, testPrompt("one")); done <- p }()
			waitPrompt(t, b, "one")
			switch reason {
			case "hook exit":
				cancel()
			case "session end":
				b.EndPrompts("session", "/repo")
			case "broker close":
				b.cancel()
			}
			select {
			case p := <-done:
				if p.Behavior != "" {
					t.Fatal(p)
				}
			case <-time.After(time.Second):
				t.Fatal("not retired")
			}
			if err := b.AnswerPrompt(context.Background(), "one", true); err == nil {
				t.Fatal("late answer allowed")
			}
		})
	}
}
func TestPromptChatCaptureAndGate(t *testing.T) {
	for _, mode := range []Mode{Confirm, Auto, FullAuto} {
		for _, allow := range []bool{false, true} {
			t.Run(string(mode)+map[bool]string{false: " deny", true: " allow"}[allow], func(t *testing.T) {
				b, r := actionTurn(t)
				ctx := context.Background()
				done := make(chan PromptDecision, 1)
				go func() { p, _ := b.WaitPrompt(ctx, testPrompt("one")); done <- p }()
				waitPrompt(t, b, "one")
				in := Approval{Conversation: r.Conversation, Token: r.Token, Tool: "mcp__driga__answer_prompt", Input: json.RawMessage(`{"id":"one","allow":true}`), ToolUseID: "call"}
				for _, snippets := range []bool{false, true} {
					b.mu.Lock()
					b.opts.Snippets = snippets
					b.mu.Unlock()
					ps, err := ChatPrompts(ctx, r.Socket, in)
					if err != nil || len(ps) != 1 || (len(ps[0].Input) > 0) != snippets {
						t.Fatal(ps, err)
					}
				}
				if _, err := ChatPrompts(ctx, r.Socket, Approval{}); err == nil {
					t.Fatal("no token accepted")
				}
				if err := b.SetMode(ctx, r.Conversation, mode); err != nil {
					t.Fatal(err)
				}
				if err := AnswerApprovedPrompt(ctx, r.Socket, in, "invented"); err == nil {
					t.Fatal("ungated answer")
				}
				permissions := make(chan Permission, 1)
				go func() { p, _ := Bridge(ctx, r.Socket, in); permissions <- p }()
				{
					a := waitAction(t, b, r.Conversation)
					if err := b.Decide(ctx, r.Conversation, a.ID, allow); err != nil {
						t.Fatal(err)
					}
				}
				p := <-permissions
				if !allow {
					if p.Behavior != "deny" {
						t.Fatal(p)
					}
					b.EndPrompts("session", "/repo")
					<-done
					return
				}
				if p.Behavior != "allow" {
					t.Fatal(p)
				}
				id, err := Claim(ctx, r.Socket, in)
				if err != nil {
					t.Fatal(err)
				}
				if err = AnswerApprovedPrompt(ctx, r.Socket, in, id); err != nil {
					t.Fatal(err)
				}
				if err = AnswerApprovedPrompt(ctx, r.Socket, in, id); err == nil {
					t.Fatal("action replayed")
				}
				if p := <-done; p.Behavior != "allow" {
					t.Fatal(p)
				}
				rows, err := b.st.ChatMessages(r.Conversation)
				if err != nil {
					t.Fatal(err)
				}
				raw, _ := json.Marshal(rows)
				if strings.Contains(string(raw), "private prompt input") {
					t.Fatal("prompt persisted")
				}
			})
		}
	}
}

func TestPromptCollectedSessionEnd(t *testing.T) {
	b, _, st := fixture(t, true)
	ctx := context.Background()
	done := make(chan PromptDecision, 1)
	go func() { p, _ := b.WaitPrompt(ctx, testPrompt("one")); done <- p }()
	waitPrompt(t, b, "one")
	now := time.Now()
	if err := st.Tx(ctx, func(tx *store.Tx) error {
		_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "session", Cwd: "/repo"}, EndedAt: &now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case p := <-done:
		if p.Behavior != "" {
			t.Fatal(p)
		}
	case <-time.After(7 * time.Second):
		t.Fatal("collected session end did not retire prompt")
	}
}

func TestPromptGenericDetailDecodesUnicode(t *testing.T) {
	p := Prompt{Tool: "OtherTool", Input: json.RawMessage(`{"value":"\u754c \u003e"}`)}
	if !strings.Contains(p.Detail(), "界 >") || !strings.Contains(p.InputText(), `\u003e`) {
		t.Fatal(p.Detail(), p.InputText())
	}
}
