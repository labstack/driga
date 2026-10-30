package cleanup

import (
	"context"
	"testing"
	"time"
)

func filled(t *testing.T, cache *SizeCache, rows []Candidate) []Candidate {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		out := cache.Snapshot(rows)
		ready := true
		for _, c := range out {
			if c.Status == "ready" && !c.SizeKnown {
				ready = false
			}
		}
		if ready {
			return out
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("size cache did not fill")
	return nil
}

func TestSizeCacheBoundedAndCancelled(t *testing.T) {
	started := make(chan struct{}, 10)
	cache := SizeCache{walk: func(ctx context.Context, _ string) (int64, bool, error) {
		started <- struct{}{}
		<-ctx.Done()
		return 12, true, nil
	}}
	rows := make([]Candidate, 6)
	for i := range rows {
		rows[i] = Candidate{ID: string(rune('a' + i)), Path: t.TempDir(), Status: "ready"}
	}
	cache.Snapshot(rows)
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("walk not started")
		}
	}
	cache.mu.Lock()
	if cache.workers != 2 {
		t.Fatalf("workers: %d", cache.workers)
	}
	cache.mu.Unlock()
	cache.Snapshot(nil)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		cache.mu.Lock()
		done := cache.workers == 0 && len(cache.entries) == 0
		cache.mu.Unlock()
		if done {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("removed work was not cancelled")
}

func TestSizeCacheCapped(t *testing.T) {
	cache := SizeCache{walk: func(context.Context, string) (int64, bool, error) { return 42, true, nil }}
	rows := []Candidate{{ID: "cap", Path: t.TempDir(), Status: "ready"}}
	out := filled(t, &cache, rows)
	if out[0].Size != 42 || !out[0].SizeCapped {
		t.Fatalf("lost lower bound: %+v", out)
	}
}
