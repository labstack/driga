package chat

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/store"
)

func TestPromptHistoryFailureCannotBeAnswered(t *testing.T) {
	b, _, st := fixture(t, false)
	state, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	b.opts.ContextStore = state
	prompt := testPrompt("history-conflict")
	locked, release := make(chan struct{}), make(chan struct{})
	txDone := make(chan error, 1)
	go func() {
		txDone <- st.Tx(context.Background(), func(*store.Tx) error { close(locked); <-release; return nil })
	}()
	<-locked
	type result struct {
		decision PromptDecision
		err      error
	}
	done := make(chan result, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { decision, err := b.WaitPrompt(ctx, prompt); done <- result{decision, err} }()
	// The independent state connection lets registration reach the blocked
	// history write. The prompt must remain unanswerable throughout that wait.
	time.Sleep(30 * time.Millisecond)
	prompts, err := b.Prompts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 0 {
		t.Errorf("prompt published before its history was persisted: %v", prompts)
	}
	if err := b.AnswerPrompt(context.Background(), prompt.ID, true); err == nil {
		t.Error("answer accepted before history persistence")
	}
	cancel() // A failed history write must not be replaced by an early answer.
	close(release)
	if err := <-txDone; err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got.err == nil || got.decision.Behavior != "" {
			t.Fatalf("history failure swallowed by answer: %+v %v", got.decision, got.err)
		}
	case <-time.After(time.Second):
		t.Fatal("failed history registration did not return")
	}
	periods, err := st.PromptIntervals(time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(periods) != 0 {
		t.Fatalf("failed registration created history: %+v", periods)
	}
}

func TestPromptHistoryFailureLogsAreSafe(t *testing.T) {
	for _, operation := range []string{"start", "end"} {
		t.Run(operation, func(t *testing.T) {
			b, _, st := fixture(t, false)
			var log bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
			defer slog.SetDefault(previous)
			prompt := testPrompt("private-prompt-identifier")
			if operation == "start" {
				if err := st.Close(); err != nil {
					t.Fatal(err)
				}
				if decision, err := b.WaitPrompt(context.Background(), prompt); err == nil || decision.Behavior != "" {
					t.Fatalf("failed history start: %+v %v", decision, err)
				}
				prompts, err := b.Prompts(context.Background())
				if err != nil || len(prompts) != 0 {
					t.Fatalf("failed history published prompt: %+v %v", prompts, err)
				}
			} else {
				done := make(chan error, 1)
				go func() {
					decision, err := b.WaitPrompt(context.Background(), prompt)
					if err == nil && decision.Behavior != "allow" {
						err = context.Canceled
					}
					done <- err
				}()
				waitPrompt(t, b, prompt.ID)
				if err := st.Close(); err != nil {
					t.Fatal(err)
				}
				if err := b.AnswerPrompt(context.Background(), prompt.ID, true); err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-done:
					if err != nil {
						t.Fatalf("committed answer lost on history retirement failure: %v", err)
					}
				case <-time.After(time.Second):
					t.Fatal("prompt did not retire")
				}
			}
			text := log.String()
			if !strings.Contains(text, "Chat prompt history failed") || !strings.Contains(text, "operation="+operation) || !strings.Contains(text, "error_type=") {
				t.Fatalf("missing safe history failure log: %s", text)
			}
			for _, secret := range []string{prompt.ID, "private prompt input", "database is closed"} {
				if strings.Contains(text, secret) {
					t.Fatalf("history log exposed %q: %s", secret, text)
				}
			}
		})
	}
}
