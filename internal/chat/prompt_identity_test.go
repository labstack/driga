package chat_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

func identityBroker(t *testing.T, collected *store.Store) (*chat.Broker, *store.Store) {
	t.Helper()
	history, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	b, err := chat.New(context.Background(), history, nil, chat.Options{DataDir: t.TempDir(), ContextStore: collected})
	if err != nil {
		history.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close(); history.Close() })
	return b, history
}

func identityPrompt(t *testing.T, b *chat.Broker, id string) (chat.Prompt, <-chan error) {
	t.Helper()
	done := make(chan error, 1)
	finished := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Error("prompt waiter did not finish during cleanup")
		}
	})
	go func() {
		defer close(finished)
		_, err := b.WaitPrompt(ctx, chat.Prompt{ID: id, Session: "discovered-session", Cwd: "/project", Tool: "Bash", Input: json.RawMessage(`{"command":"private input"}`)})
		done <- err
	}()
	return awaitIdentityPrompt(t, b, func(p chat.Prompt) bool { return p.ID == id }), done
}

func awaitIdentityPrompt(t *testing.T, b *chat.Broker, match func(chat.Prompt) bool) chat.Prompt {
	t.Helper()
	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		prompts, err := b.Prompts(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range prompts {
			if match(p) {
				return p
			}
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("prompt identity was not resolved")
		}
	}
}

func TestDiscoveredPromptSessionLinksDurableHistoryAndCountsOnce(t *testing.T) {
	b, st := identityBroker(t, nil)
	prompt, done := identityPrompt(t, b, "before-collection")
	if prompt.SessionID != 0 {
		t.Fatal("fixture already knew session", prompt.SessionID)
	}
	var id int64
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		var err error
		id, err = tx.UpsertSession(model.Session{Label: "waiting", SessionObs: model.SessionObs{Adapter: "claude", ExternalID: prompt.Session, Cwd: prompt.Cwd}})
		if err != nil {
			return err
		}
		return tx.UpsertInboxItem(model.InboxItem{ID: "notification", SessionID: id, Kind: "waiting", TS: prompt.CreatedAt})
	}); err != nil {
		t.Fatal(err)
	}
	awaitIdentityPrompt(t, b, func(p chat.Prompt) bool { return p.ID == prompt.ID && p.SessionID == id })
	periods, err := st.PromptIntervals(time.Time{})
	if err != nil || len(periods) != 1 || periods[0].SessionID != id || !periods[0].StartedAt.Equal(prompt.CreatedAt) {
		t.Fatalf("discovered identity did not reach its durable interval: %+v %v", periods, err)
	}
	history, err := view.PanelHistory(st, view.Filter{}, time.Now().UTC())
	if err != nil || len(history.Attention) != 60 || history.Attention[59].Waiting != 1 {
		t.Fatalf("prompt and notification were counted twice: %+v %v", history.Attention, err)
	}
	if err := b.AnswerPrompt(context.Background(), prompt.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestPromptIdentityHistoryWriteDoesNotHoldBrokerLockOrResurrectRetiredPrompt(t *testing.T) {
	collected, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer collected.Close()
	b, history := identityBroker(t, collected)
	prompt, done := identityPrompt(t, b, "retire-during-link")
	var id int64
	if err := collected.Tx(context.Background(), func(tx *store.Tx) error {
		var err error
		id, err = tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: prompt.Session, Cwd: prompt.Cwd}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	locked, release := make(chan struct{}), make(chan struct{})
	transaction := make(chan error, 1)
	go func() {
		transaction <- history.Tx(context.Background(), func(*store.Tx) error { close(locked); <-release; return nil })
	}()
	<-locked
	// Refresh uses the shared five-second tick. Block only history, so the
	// collected-state lookup still discovers the session and reaches linking.
	time.Sleep(5500 * time.Millisecond)
	responsive := make(chan error, 1)
	go func() {
		_, err := b.Prompts(context.Background())
		if err == nil {
			err = b.AnswerPrompt(context.Background(), prompt.ID, false)
		}
		responsive <- err
	}()
	var responseErr error
	select {
	case responseErr = <-responsive:
	case <-time.After(300 * time.Millisecond):
		t.Error("blocked identity history write held the broker lock")
	}
	close(release)
	if err := <-transaction; err != nil {
		t.Fatal(err)
	}
	if responseErr != nil {
		t.Fatal(responseErr)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("retiring prompt did not finish")
	}
	// The answer can win the database connection ahead of the queued link.
	// Let the original refresh finish before cancelling the broker context.
	deadline := time.Now().Add(time.Second)
	for {
		periods, err := history.PromptIntervals(time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		if len(periods) == 1 && periods[0].SessionID == id {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("retired interval did not receive the in-flight link: %+v", periods)
		}
		time.Sleep(time.Millisecond)
	}
	b.Close() // Also waits for the in-flight refresh to finish.
	ps, err := b.Prompts(context.Background())
	if err != nil || len(ps) != 0 {
		t.Fatalf("identity link resurrected a retired prompt: %+v %v", ps, err)
	}
	periods, err := history.PromptIntervals(time.Time{})
	if err != nil || len(periods) != 1 || periods[0].EndedAt == nil || periods[0].SessionID != id {
		t.Fatalf("retired interval lost its identity or was reopened: %+v %v", periods, err)
	}
}

func TestPromptHistoryIdentityLinkBindsTheExactRegistration(t *testing.T) {
	_, history := identityBroker(t, nil)
	ctx := context.Background()
	old, current := time.Now().UTC().Add(-time.Minute), time.Now().UTC()
	for _, at := range []time.Time{old, current} {
		if err := history.StartPromptHistory(ctx, "reused", 0, at); err != nil {
			t.Fatal(err)
		}
	}
	if err := history.LinkPromptHistory(ctx, "reused", old, 21); err != nil {
		t.Fatal(err)
	}
	intervals, err := history.PromptIntervals(time.Time{})
	if err != nil || len(intervals) != 2 {
		t.Fatalf("registration intervals=%+v %v", intervals, err)
	}
	for _, interval := range intervals {
		want := int64(0)
		if interval.StartedAt.Equal(old) {
			want = 21
		}
		if interval.SessionID != want {
			t.Fatalf("identity link changed another registration: %+v", intervals)
		}
	}
}
