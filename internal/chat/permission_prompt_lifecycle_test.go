package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestPromptApprovalTitle(t *testing.T) {
	for _, allow := range []bool{true, false} {
		t.Run(fmt.Sprint(allow), func(t *testing.T) {
			b, r := actionTurn(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go b.WaitPrompt(ctx, testPrompt("target"))
			waitPrompt(t, b, "target")
			in := Approval{Conversation: r.Conversation, Token: r.Token, Tool: "mcp__driga__answer_prompt", Input: json.RawMessage(fmt.Sprintf(`{"id":"target","allow":%t}`, allow))}
			done := make(chan Permission, 1)
			go func() { p, _ := b.Approve(ctx, in); done <- p }()
			card := waitAction(t, b, r.Conversation)
			decision := "Deny"
			if allow {
				decision = "Allow"
			}
			for _, text := range []string{decision, "claude session", "/repo", "Bash", "private prompt input"} {
				if !strings.Contains(card.Title, text) {
					t.Fatalf("title %q missing %q", card.Title, text)
				}
			}
			if err := b.Decide(ctx, r.Conversation, card.ID, false); err != nil {
				t.Fatal(err)
			}
			<-done
			rows, err := b.st.ChatMessages(r.Conversation)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range rows {
				if m.Role == "action" && (!strings.Contains(m.Text, "answer_prompt target") || strings.Contains(m.Text, "private prompt input")) {
					t.Fatal("prompt audit lost its identity or retained input", m)
				}
			}
		})
	}
}

func TestPromptsListingDoesNotQueryStore(t *testing.T) {
	b, _, st := fixture(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.WaitPrompt(ctx, testPrompt("one"))
	waitPrompt(t, b, "one")
	locked, release, released := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() { released <- st.Tx(ctx, func(*store.Tx) error { close(locked); <-release; return nil }) }()
	<-locked
	read := make(chan error, 1)
	go func() {
		ps, err := b.Prompts(ctx)
		if err == nil && len(ps) != 1 {
			err = fmt.Errorf("missing prompt: %v", ps)
		}
		read <- err
	}()
	var err error
	select {
	case err = <-read:
	case <-time.After(300 * time.Millisecond):
		err = fmt.Errorf("prompt listing waited for store")
	}
	close(release)
	<-released
	if err != nil {
		t.Fatal(err)
	}
}

func TestPromptSessionChecksAreThrottled(t *testing.T) {
	b, _, st := fixture(t, true)
	ctx := context.Background()
	done := make(chan PromptDecision, 2)
	for _, id := range []string{"one", "two"} {
		go func() { p, _ := b.WaitPrompt(ctx, testPrompt(id)); done <- p }()
		waitPrompt(t, b, id)
	}
	now := time.Now()
	if err := st.Tx(ctx, func(tx *store.Tx) error {
		_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "session", Cwd: "/repo"}, EndedAt: &now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
		t.Fatal("session checks ran before the shared five-second tick")
	case <-time.After(1200 * time.Millisecond):
	}
	// The shared ticker must retire both, while the SessionEnd hook stays immediate.
	for range 2 {
		select {
		case p := <-done:
			if p.Behavior != "" {
				t.Fatal(p)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("shared session check did not retire prompt")
		}
	}
}

func TestPromptCancellationAndAnswerAtomic(t *testing.T) {
	for _, answerFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(answerFirst), func(t *testing.T) {
			b, _, _ := fixture(t, true)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan PromptDecision, 1)
			go func() { p, _ := b.WaitPrompt(ctx, testPrompt("race")); done <- p }()
			waitPrompt(t, b, "race")
			b.mu.Lock()
			if !answerFirst {
				cancel()
			}
			err := b.answerPrompt("race", true)
			cancel()
			b.mu.Unlock()
			p := <-done
			if answerFirst {
				if err != nil || p.Behavior != "allow" {
					t.Fatal(p, err)
				}
			} else if err == nil || err.Error() != "prompt no longer waiting" || p.Behavior != "" {
				t.Fatal(p, err)
			}
		})
	}
}

func TestRetiredPromptActionCard(t *testing.T) {
	b, r := actionTurn(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { b.WaitPrompt(ctx, testPrompt("gone")); close(done) }()
	waitPrompt(t, b, "gone")
	if err := b.SetMode(context.Background(), r.Conversation, FullAuto); err != nil {
		t.Fatal(err)
	}
	in := Approval{Conversation: r.Conversation, Token: r.Token, Tool: "mcp__driga__answer_prompt", Input: json.RawMessage(`{"id":"gone","allow":true}`)}
	permission := make(chan Permission, 1)
	go func() { p, _ := b.Approve(context.Background(), in); permission <- p }()
	card := waitAction(t, b, r.Conversation)
	if err := b.Decide(context.Background(), r.Conversation, card.ID, true); err != nil {
		t.Fatal(err)
	}
	if p := <-permission; p.Behavior != "allow" {
		t.Fatal(p)
	}
	id, err := b.Claim(in)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	<-done
	if err := b.AnswerApprovedPrompt(in, id); err == nil || err.Error() != "prompt no longer waiting or changed" {
		t.Fatal(err)
	}
	s, err := b.Read(context.Background(), r.Conversation, 0)
	if err != nil {
		t.Fatal(err)
	}
	if s.Actions[0].Status != "failed" || s.Actions[0].Result != "prompt no longer waiting or changed" {
		t.Fatal(s.Actions)
	}
}
