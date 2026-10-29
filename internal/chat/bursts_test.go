package chat

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func burstPrompt(t *testing.T, b *Broker, id, session, cwd string) <-chan PromptDecision {
	t.Helper()
	done := make(chan PromptDecision, 1)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { p := testPrompt(id); p.Session = session; p.Cwd = cwd; v, _ := b.WaitPrompt(ctx, p); done <- v }()
	waitPrompt(t, b, id)
	return done
}
func TestPromptBurstWindow(t *testing.T) {
	b, _, _ := fixture(t, true)
	var millis atomic.Int64
	base := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	b.opts.Now = func() time.Time { return base.Add(time.Duration(millis.Load()) * time.Millisecond) }
	burstPrompt(t, b, "one", "s", "/repo")
	millis.Store(2999)
	burstPrompt(t, b, "two", "s", "/repo")
	millis.Store(3000)
	burstPrompt(t, b, "three", "s", "/repo")
	burstPrompt(t, b, "cwd", "s", "/other")
	burstPrompt(t, b, "session", "other", "/repo")
	groups, e := b.PromptBursts(context.Background())
	if e != nil || len(groups) != 4 {
		t.Fatal(groups, e)
	}
	found := false
	for _, g := range groups {
		if len(g.Members) == 2 {
			found = true
			if !g.OpenedAt.Equal(base) || g.Members[0].ID != "one" || g.Members[1].ID != "two" || g.Members[0].InputHash == "" {
				t.Fatal(g)
			}
		}
	}
	if !found {
		t.Fatal(groups)
	}
}
func TestPromptBurstDecision(t *testing.T) {
	b, _, _ := fixture(t, true)
	ctx := context.Background()
	one := burstPrompt(t, b, "one", "s", "/repo")
	two := burstPrompt(t, b, "two", "s", "/repo")
	groups, _ := b.PromptBursts(ctx)
	g := groups[0]
	late := burstPrompt(t, b, "late", "s", "/repo")
	if e := b.AnswerPrompt(ctx, "two", false); e != nil {
		t.Fatal(e)
	}
	if (<-two).Behavior != "deny" {
		t.Fatal("individual deny lost")
	}
	if e := b.DecideBurst(ctx, g.ID, []string{"one", "two"}, true); e == nil || !strings.Contains(e.Error(), "stale") {
		t.Fatal("stale member not reported", e)
	}
	if (<-one).Behavior != "allow" {
		t.Fatal("visible live member denied")
	}
	select {
	case <-late:
		t.Fatal("late arrival answered")
	default:
	}
	if e := b.DecideBurst(ctx, g.ID, []string{"one"}, true); e == nil {
		t.Fatal("duplicate decision")
	}
	b.EndPrompts("s", "/repo")
	<-late
	replacement := burstPrompt(t, b, "replacement", "s", "/repo")
	if e := b.DecideBurst(ctx, g.ID, []string{"replacement"}, true); e == nil {
		t.Fatal("session reuse inherited old group")
	}
	_ = b.AnswerPrompt(ctx, "replacement", false)
	<-replacement
}
func TestPromptBurstInputBinding(t *testing.T) {
	b, _, _ := fixture(t, true)
	ctx := context.Background()
	done := burstPrompt(t, b, "one", "s", "/repo")
	g, _ := b.PromptBursts(ctx)
	g[0].Members[0].Prompt.Input[0] = 'x'
	again, _ := b.PromptBursts(ctx)
	if !json.Valid(again[0].Members[0].Prompt.Input) {
		t.Fatal("client mutated live input")
	}
	b.mu.Lock()
	b.prompts["one"].Input = json.RawMessage(`{"command":"changed"}`)
	b.mu.Unlock()
	if e := b.DecideBurst(ctx, g[0].ID, []string{"one"}, true); e == nil {
		t.Fatal("changed input allowed")
	}
	select {
	case <-done:
		t.Fatal("changed input answered")
	default:
	}
	_ = b.AnswerPrompt(ctx, "one", false)
	<-done
}
func TestPromptBurstCapability(t *testing.T) {
	b, in, _ := commandTurn(t)
	for _, path := range []string{"/prompt-bursts", "/burst-decide"} {
		r := httptest.NewRequest("POST", path, strings.NewReader(`{"id":"x","members":["p"],"allow":true,"similar":true}`))
		r.Header.Set("Authorization", in.Token)
		w := httptest.NewRecorder()
		handler(b).ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal(path, w.Code)
		}
	}
}

func TestPromptBurstRetirement(t *testing.T) {
	for _, kind := range []string{"disconnect", "timeout"} {
		t.Run(kind, func(t *testing.T) {
			b, _, _ := fixture(t, true)
			if kind == "timeout" {
				b.promptTTL = 40 * time.Millisecond
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() { defer close(done); _, _ = b.WaitPrompt(ctx, testPrompt("retiring")) }()
			waitPrompt(t, b, "retiring")
			if kind == "disconnect" {
				cancel()
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("prompt did not retire")
			}
			g, err := b.PromptBursts(context.Background())
			if err != nil || len(g) != 0 {
				t.Fatal("membership survived hook lifetime", g, err)
			}
		})
	}
}
