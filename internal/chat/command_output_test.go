package chat

import (
	"bytes"
	"context"
	"github.com/labstack/driga/internal/store"
	"io"
	"strings"
	"testing"
	"time"
)

func outputFixture(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	now := time.Now()
	if err = st.SaveConversation(ctx, store.Conversation{ID: "c", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err = st.SaveChatMessage(ctx, store.ChatMessage{ID: "a", ConversationID: "c", Role: "action", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	return st
}
func TestCommandOutputCapture(t *testing.T) {
	for _, snippets := range []bool{false, true} {
		t.Run(map[bool]string{false: "titles", true: "snippets"}[snippets], func(t *testing.T) {
			st := outputFixture(t)
			out := newCommandOutput(st, "a", snippets)
			w := out.Writer("stdout")
			for _, part := range []string{"HEAD ", "ghp_", "testsecret\n", strings.Repeat("middle ", 3000), strings.Repeat("z", 20000) + " ", "TAIL\n"} {
				if _, err := io.WriteString(w, part); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := io.WriteString(out.Writer("stderr"), "ERR\n"); err != nil {
				t.Fatal(err)
			}
			summary, err := out.Finish(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			var full bytes.Buffer
			if err = st.ReadCommandOutput(context.Background(), "a", &full); err != nil {
				t.Fatal(err)
			}
			// The agent gets the scrubbed head and tail under every policy.
			if strings.Contains(summary, "ghp_") || !strings.Contains(summary, "HEAD") || !strings.Contains(summary, "[redacted]") || !strings.Contains(summary, "TAIL") || len(summary) > 5000 {
				t.Fatal("agent summary", summary)
			}
			if !snippets {
				if full.Len() != 0 {
					t.Fatal("titles retained output")
				}
				return
			}
			if strings.Contains(full.String(), "ghp_") || !strings.Contains(full.String(), "[redacted]") || !strings.Contains(full.String(), strings.Repeat("middle ", 3000)) || !strings.Contains(full.String(), strings.Repeat("z", 20000)) {
				t.Fatal("full scrubbed output lost")
			}
			if !strings.Contains(summary, "HEAD") || !strings.Contains(summary, "TAIL") || !strings.Contains(summary, "ERR") || len(summary) > 5000 {
				t.Fatal("head/tail lost", summary)
			}
		})
	}
}
func TestCommandOutputFailure(t *testing.T) {
	st := outputFixture(t)
	out := newCommandOutput(st, "a", true)
	st.Close()
	if _, err := io.WriteString(out.Writer("stdout"), "output\n"); err == nil {
		t.Fatal("storage failure hidden")
	}
	if _, err := out.Finish(context.Background()); err == nil {
		t.Fatal("finish lost writer failure")
	}
}
