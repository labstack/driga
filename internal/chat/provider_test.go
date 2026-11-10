package chat

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/labstack/driga/internal/store"
)

type providerAgentFunc func(context.Context, Request, func(Delta) error) error

func (f providerAgentFunc) Run(ctx context.Context, r Request, emit func(Delta) error) error {
	return f(ctx, r, emit)
}

func TestProviderSelectionOrderAndExplicitOverride(t *testing.T) {
	for _, tt := range []struct {
		name, preference, available, want string
		calls                             []string
	}{
		{"first", "auto", "claude", "claude", []string{"claude"}},
		{"skip incompatible", "auto", "codex", "codex", []string{"claude", "codex"}},
		{"last", "auto", "copilot", "copilot", []string{"claude", "codex", "copilot"}},
		{"none", "auto", "", "", []string{"claude", "codex", "copilot"}},
		{"explicit available", "copilot", "copilot", "copilot", []string{"copilot"}},
		{"explicit unavailable", "claude", "codex", "", []string{"claude"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var calls []string
			p := &providers{preference: tt.preference, probe: func(_ context.Context, name string) (Agent, error) {
				calls = append(calls, name)
				if name != tt.available {
					return nil, errors.New("incompatible fixture CLI")
				}
				return providerAgentFunc(func(context.Context, Request, func(Delta) error) error { return nil }), nil
			}}
			got, err := p.Select(context.Background())
			if got != tt.want || (err != nil) != (tt.want == "") || !reflect.DeepEqual(calls, tt.calls) {
				t.Fatalf("selection=%q err=%v calls=%v; want=%q calls=%v", got, err, calls, tt.want, tt.calls)
			}
		})
	}
}

func TestProviderSelectionCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := &providers{preference: "auto", probe: func(context.Context, string) (Agent, error) {
		t.Fatal("canceled selection probed a provider")
		return nil, nil
	}}
	if _, err := p.Select(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("selection err=%v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	p.probe = func(context.Context, string) (Agent, error) {
		cancel()
		return nil, ctx.Err()
	}
	if _, err := p.Select(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted selection err=%v", err)
	}
}

func TestProviderRunNeverFallsBackOrChangesResume(t *testing.T) {
	var calls []string
	wantErr := errors.New("provider unavailable")
	p := &providers{preference: "auto", probe: func(_ context.Context, name string) (Agent, error) {
		calls = append(calls, name)
		return nil, wantErr
	}}
	request := Request{Provider: "codex", Resume: "codex-private-session", Prompt: "private question"}
	if err := p.Run(context.Background(), request, func(Delta) error { t.Fatal("failed provider emitted output"); return nil }); !errors.Is(err, wantErr) {
		t.Fatalf("run err=%v", err)
	}
	if !reflect.DeepEqual(calls, []string{"codex"}) {
		t.Fatalf("failed turn tried providers %v", calls)
	}
	p.probe = func(_ context.Context, name string) (Agent, error) {
		if name != "codex" {
			t.Fatalf("resume routed to %s", name)
		}
		return providerAgentFunc(func(_ context.Context, got Request, _ func(Delta) error) error {
			if got.Provider != request.Provider || got.Resume != request.Resume || got.Prompt != request.Prompt {
				t.Fatalf("request changed: %+v", got)
			}
			return nil
		}), nil
	}
	if err := p.Run(context.Background(), request, func(Delta) error { return nil }); err != nil {
		t.Fatal(err)
	}
	for _, unpinned := range []string{"", "auto"} {
		if err := p.Run(context.Background(), Request{Provider: unpinned}, func(Delta) error { return nil }); err == nil {
			t.Fatal("unpinned conversation ran")
		}
	}
}

func providerBroker(t *testing.T, p Agent) (*Broker, *store.Store) {
	t.Helper()
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	b, err := New(context.Background(), st, p, Options{DataDir: t.TempDir()})
	if err != nil {
		st.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close(); st.Close() })
	return b, st
}

func TestProviderBrokerSelectionIsLazyAndFailedCreateHasNoRow(t *testing.T) {
	calls := 0
	p := &providers{preference: "auto", probe: func(context.Context, string) (Agent, error) {
		calls++
		return nil, errors.New("fixture unavailable")
	}}
	b, st := providerBroker(t, p)
	if calls != 0 {
		t.Fatalf("broker startup probed %d providers", calls)
	}
	if _, err := b.Create(context.Background()); err == nil {
		t.Fatal("create succeeded without provider")
	}
	if rows, err := st.ChatConversations(); err != nil || len(rows) != 0 {
		t.Fatalf("failed selection persisted rows=%+v err=%v", rows, err)
	}
}

func TestProviderBrokerProbesOutsideConversationLock(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	p := &providers{preference: "codex", probe: func(ctx context.Context, _ string) (Agent, error) {
		close(entered)
		select {
		case <-release:
			return nil, errors.New("fixture unavailable")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}
	b, st := providerBroker(t, p)
	if err := st.SaveConversation(context.Background(), store.Conversation{ID: "existing", Agent: "claude", CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); _, _ = b.Create(ctx) }()
	<-entered
	read := make(chan error, 1)
	go func() { _, err := b.Read(context.Background(), "existing", 0); read <- err }()
	select {
	case err := <-read:
		if err != nil {
			close(release)
			<-done
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		close(release)
		<-done
		t.Fatal("provider probe blocked existing conversation reads")
	}
	close(release)
	<-done
}

func TestProviderBrokerPinsNewAndLegacyConversationRequests(t *testing.T) {
	var mu sync.Mutex
	var requests []Request
	p := &providers{preference: "codex", probe: func(_ context.Context, name string) (Agent, error) {
		return providerAgentFunc(func(_ context.Context, r Request, emit func(Delta) error) error {
			mu.Lock()
			requests = append(requests, r)
			mu.Unlock()
			return emit(Delta{SessionID: name + "-session", Text: "fixture answer"})
		}), nil
	}}
	b, st := providerBroker(t, p)
	ctx := context.Background()
	c, err := b.Create(ctx)
	if err != nil || c.Agent != "codex" {
		t.Fatalf("new conversation=%+v err=%v", c, err)
	}
	if err := b.Send(ctx, c.ID, "first", Context{}); err != nil {
		t.Fatal(err)
	}
	settled(t, b, c.ID)
	if err := b.Send(ctx, c.ID, "resume", Context{}); err != nil {
		t.Fatal(err)
	}
	settled(t, b, c.ID)
	if err := st.SaveConversation(ctx, store.Conversation{ID: "legacy", Agent: "claude", SessionID: "original-claude-id", CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := b.Send(ctx, "legacy", "continue", Context{}); err != nil {
		t.Fatal(err)
	}
	settled(t, b, "legacy")
	b.Close()
	next, err := New(ctx, st, p, Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if err := next.Send(ctx, c.ID, "after broker restart", Context{}); err != nil {
		t.Fatal(err)
	}
	settled(t, next, c.ID)
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 4 || requests[0].Provider != "codex" || requests[0].Resume != "" || requests[1].Provider != "codex" || requests[1].Resume != "codex-session" || requests[2].Provider != "claude" || requests[2].Resume != "original-claude-id" || requests[3].Provider != "codex" || requests[3].Resume != "codex-session" {
		t.Fatalf("routed requests=%+v", requests)
	}
}
