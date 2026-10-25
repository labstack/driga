package web

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/chat"
)

func webPrompt(t *testing.T, b *chat.Broker) <-chan chat.PromptDecision {
	t.Helper()
	done := make(chan chat.PromptDecision, 1)
	go func() {
		p, _ := b.WaitPrompt(context.Background(), chat.Prompt{ID: "prompt", Session: "session", Cwd: "/repo", Tool: "Bash", Input: json.RawMessage(`{"command":"<script>private</script>"}`)})
		done <- p
	}()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		ps, _ := b.Prompts(context.Background())
		if len(ps) > 0 {
			return done
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no prompt")
	return done
}
func TestInboxPromptDecisionsAndAuth(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(map[bool]string{true: "allow", false: "deny"}[allow], func(t *testing.T) {
			s, b, _ := chatFixture(t)
			done := webPrompt(t, b)
			var listed []chat.Prompt
			if w := request(s, "GET", "/chat/api/prompts"); json.Unmarshal(w.Body.Bytes(), &listed) != nil || len(listed) != 1 || listed[0].ID != "prompt" || strings.Contains(w.Body.String(), "<script>") {
				t.Fatal("prompts:", w.Body.String())
			}
			body := `{"id":"prompt","allow":false}`
			if allow {
				body = `{"id":"prompt","allow":true}`
			}
			if w := request(s, "GET", "/chat/api/prompt-answer"); w.Code != 405 {
				t.Fatal(w.Code)
			}
			w := postChat(s, "/chat/api/prompt-answer", body)
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			select {
			case p := <-done:
				if (p.Behavior == "allow") != allow {
					t.Fatal(p)
				}
			case <-time.After(time.Second):
				t.Fatal("not answered")
			}
			w = postChat(s, "/chat/api/prompt-answer", body)
			if w.Code != 400 || !strings.Contains(w.Body.String(), "prompt no longer waiting") {
				t.Fatal(w.Code, w.Body.String())
			}
			if w := request(s, "GET", "/chat/api/prompts"); strings.Contains(w.Body.String(), `"prompt"`) {
				t.Fatal("retained prompt", w.Body.String())
			}
		})
	}
}
