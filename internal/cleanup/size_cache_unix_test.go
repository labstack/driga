//go:build !windows

package cleanup

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSizeCacheReadyCandidates(t *testing.T) {
	e, _, _, _ := fixture(t)
	a, b := scratch(t, e, "size-a"), scratch(t, e, "size-b")
	put(t, filepath.Join(a, "file"), "abc")
	put(t, filepath.Join(b, "file"), "12345")
	rows := []Candidate{listed(t, e, a), listed(t, e, b),
		{ID: "refused", Path: a, Status: "refused"}, {ID: "history", Path: b, Status: "report_only"}}
	var cache SizeCache
	first := cache.Snapshot(rows)
	if first[0].SizeKnown || first[1].SizeKnown {
		t.Fatal("first load should be unknown")
	}
	out := filled(t, &cache, rows)
	if out[0].Size+out[1].Size != 8 {
		t.Fatalf("sizes: %+v", out)
	}
	if out[2].SizeKnown || out[3].SizeKnown {
		t.Fatal("measured excluded candidates")
	}
	cache.mu.Lock()
	if len(cache.entries) != 2 {
		t.Fatal("cached excluded candidates")
	}
	cache.mu.Unlock()
	cache.Snapshot(rows[1:])
	cache.mu.Lock()
	if len(cache.entries) != 1 {
		t.Fatal("removed candidate remained cached")
	}
	cache.mu.Unlock()
	if cache.Snapshot(rows)[0].SizeKnown {
		t.Fatal("disappeared candidate reused stale size")
	}
	filled(t, &cache, rows)
	// Directory mtime invalidates immediately; nested modifications refresh by TTL.
	at := time.Now().Add(time.Hour)
	if err := os.Chtimes(a, at, at); err != nil {
		t.Fatal(err)
	}
	if cache.Snapshot(rows)[0].SizeKnown {
		t.Fatal("mtime change reused stale size")
	}
	filled(t, &cache, rows)
	cache.mu.Lock()
	for _, entry := range cache.entries {
		entry.at = time.Now().Add(-11 * time.Minute)
	}
	cache.mu.Unlock()
	if cache.Snapshot(rows)[0].SizeKnown {
		t.Fatal("expired size reused")
	}
	filled(t, &cache, rows)
}
