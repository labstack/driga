package chat

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestPromptWatcherStopsTimerWhileIdle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &Broker{ctx: ctx, promptWake: make(chan struct{}, 1), prompts: map[string]*pendingPrompt{}}
	starts, stops := make(chan struct{}, 3), make(chan struct{}, 3)
	var count atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		b.watchPromptChanges(func() (<-chan time.Time, func()) {
			count.Add(1)
			starts <- struct{}{}
			// The fake clock has no events; all progress must be event driven.
			return make(chan time.Time), func() { stops <- struct{}{} }
		})
	}()
	select {
	case <-starts:
		t.Fatal("idle broker started a periodic timer")
	case <-time.After(30 * time.Millisecond):
	}
	for range 2 {
		b.mu.Lock()
		b.prompts["one"] = &pendingPrompt{}
		b.wakePromptWatcher()
		b.mu.Unlock()
		select {
		case <-starts:
		case <-time.After(time.Second):
			t.Fatal("new prompt did not start fallback")
		}
		b.mu.Lock()
		delete(b.prompts, "one")
		b.wakePromptWatcher()
		b.mu.Unlock()
		select {
		case <-stops:
		case <-time.After(time.Second):
			t.Fatal("retirement left timer running")
		}
	}
	cancel()
	<-done
	if count.Load() != 2 {
		t.Fatal("unexpected timers", count.Load())
	}
	t.Log("idle periodic prompt ticks: 0; timer starts/stops: 2/2")
}
