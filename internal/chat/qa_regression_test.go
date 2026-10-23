package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompletedActionsDoNotExhaustBudget(t *testing.T) {
	b, _, st := fixture(t, true)
	ctx := context.Background()
	row, err := b.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	c, err := b.load(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	c.cancel = func() {}
	c.token = "turn"
	b.mu.Unlock()
	for i := 0; i < MaxMessages+8; i++ {
		done := make(chan error, 1)
		go func() {
			p, err := b.Approve(ctx, Approval{Conversation: row.ID, Token: "turn", Tool: "Bash", Input: json.RawMessage(`{}`)})
			if err == nil && p.Behavior != "deny" {
				err = fmt.Errorf("unexpected permission: %s", p.Behavior)
			}
			done <- err
		}()
		// Wait for the event instead of sleeping through 256 cards.
		deadline := time.After(3 * time.Second)
		for {
			s, err := b.Read(ctx, row.ID, 0)
			if err != nil {
				t.Fatal(err)
			}
			var pending string
			for _, a := range s.Actions {
				if a.Status == "pending" {
					pending = a.ID
				}
			}
			if pending != "" {
				if err := b.Decide(ctx, row.ID, pending, false); err != nil {
					t.Fatal(err)
				}
				break
			}
			select {
			case err := <-done:
				t.Fatalf("action %d rejected: %v", i, err)
			case <-deadline:
				t.Fatal("no card")
			default:
				time.Sleep(time.Millisecond)
			}
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	s, _ := b.Read(ctx, row.ID, 0)
	if len(s.Actions) > MaxMessages {
		t.Fatal(len(s.Actions))
	}
	rows, err := st.ChatMessages(row.ID)
	if err != nil || len(rows) == 0 {
		t.Fatal(err)
	}
	if err := b.Stop(ctx, row.ID); err != nil {
		t.Fatal(err)
	}
}

func TestFullAutoEvictionDisposesTimer(t *testing.T) {
	b, _, _ := fixture(t, true)
	ctx := context.Background()
	row, _ := b.Create(ctx)
	if err := b.SetMode(ctx, row.ID, FullAuto); err != nil {
		t.Fatal(err)
	}
	old := b.conversations[row.ID]
	timer := old.expire
	for i := 0; i < MaxConversations; i++ {
		r, _ := b.Create(ctx)
		if _, err := b.Read(ctx, r.ID, 0); err != nil {
			t.Fatal(err)
		}
	}
	if b.conversations[row.ID] != nil {
		t.Fatal("not evicted")
	}
	b.Close()
	if timer.Stop() || old.expire != nil || old.mode != Confirm || len(old.events) != 0 {
		t.Fatal("evicted state retained its timer/buffers")
	}
}

func TestActionMetadataSurvivesRedactionAndRestart(t *testing.T) {
	b, _, st := fixture(t, false)
	ctx := context.Background()
	row, _ := b.Create(ctx)
	b.mu.Lock()
	c, err := b.load(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	a := &pendingAction{Action: Action{ID: "audit", Tool: "Bash", Title: "private command", Status: "denied", DecidedBy: "you", Input: json.RawMessage(`{}`)}}
	err = b.actionEvent(c, a)
	b.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	b.Close()
	next, err := New(ctx, st, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	state, err := next.Read(ctx, row.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Actions) != 1 {
		t.Fatal(state)
	}
	got := state.Actions[0]
	if got.Tool != "Bash" || got.Status != "denied" || got.DecidedBy != "you" || strings.Contains(got.Title, "private") {
		t.Fatal(got)
	}
	if state.Messages[0].Text != "" || state.Messages[0].Tool != "Bash" || state.Messages[0].DecidedBy != "you" {
		t.Fatal(state.Messages)
	}
}

func TestRemoteRefreshesRotatedKeyAndReportsHTTPError(t *testing.T) {
	key := filepath.Join(t.TempDir(), "chat.key")
	if err := os.WriteFile(key, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	var posts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "new" {
			http.Error(w, "invalid client token", 403)
			return
		}
		if r.URL.Path == "/error" {
			http.Error(w, "broker unavailable", 503)
			return
		}
		posts++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[]`)
	}))
	defer server.Close()
	c := &Remote{token: "old", keyPath: key, http: server.Client()}
	// Route synthetic IPC requests to a local test server; no vendor process.
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		r.URL.Scheme = "http"
		r.URL.Host = strings.TrimPrefix(server.URL, "http://")
		return http.DefaultTransport.RoundTrip(r)
	})
	if _, err := c.List(context.Background()); err != nil {
		t.Fatal(err)
	}
	if posts != 1 || c.token != "new" {
		t.Fatal(posts, c.token)
	}
	err := c.call(context.Background(), "GET", "/error", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 503: broker unavailable") {
		t.Fatal(err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBrokerWaitWakesAllStreamsAndCancels(t *testing.T) {
	b, _, _ := fixture(t, true)
	ctx := context.Background()
	row, _ := b.Create(ctx)
	s, _ := b.Read(ctx, row.ID, 0)
	done := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func() {
			s, err := b.Wait(ctx, row.ID, s.Cursor)
			if err == nil && s.Mode != Auto {
				err = fmt.Errorf("mode %s", s.Mode)
			}
			done <- err
		}()
	}
	if err := b.SetMode(ctx, row.ID, Auto); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("missed broker event")
		}
	}
	s, _ = b.Read(ctx, row.ID, 0)
	cancelCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, err := b.Wait(cancelCtx, row.ID, s.Cursor); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	// A stream waiting on an evicted conversation must also wake and reload.
	wait := make(chan error, 1)
	go func() { _, err := b.Wait(ctx, row.ID, s.Cursor); wait <- err }()
	b.Close()
	select {
	case <-wait:
	case <-time.After(time.Second):
		t.Fatal("Close left wait alive")
	}
}

func TestActionByteBudgetPreservesOutstanding(t *testing.T) {
	b, _, _ := fixture(t, true)
	ctx := context.Background()
	row, _ := b.Create(ctx)
	b.mu.Lock()
	c, err := b.load(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	c.cancel = func() {}
	c.token = "turn"
	c.remote = true
	pending := &pendingAction{Action: Action{ID: "pending", Status: "pending", Input: json.RawMessage(`{}`)}, answer: make(chan Permission, 1)}
	claimed := &pendingAction{Action: Action{ID: "claimed", Status: "allowed", Input: json.RawMessage(`{}`)}, claimed: true}
	c.actions = []*pendingAction{pending, claimed}
	b.mu.Unlock()
	raw, _ := json.Marshal(map[string]string{"command": strings.Repeat("x", 100000)})
	for i := 0; i < 16; i++ {
		if _, err := b.Approve(ctx, Approval{Conversation: row.ID, Token: "turn", Tool: "Bash", Input: raw}); err != nil {
			t.Fatal(i, err)
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if c.actions[0] != pending || c.actions[1] != claimed {
		t.Fatal("outstanding actions evicted")
	}
	if len(c.actions) >= 18 {
		t.Fatal("completed bytes never trimmed")
	}
}
