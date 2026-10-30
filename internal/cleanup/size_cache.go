package cleanup

import (
	"context"
	"os"
	"sync"
	"time"
)

const sizeRefreshInterval = 10 * time.Minute

type sizeKey struct {
	id, path string
	mtime    int64
}
type sizeEntry struct {
	size                            int64
	known, capped, pending, running bool
	at                              time.Time
	cancel                          context.CancelFunc
}

// SizeCache lazily measures ready candidates, with at most two directory walks.
// Snapshot only stats roots and copies cached values; recursive IO runs outside
// the caller's context. The zero value is ready to use. Do not copy after use.
type SizeCache struct {
	mu      sync.Mutex
	entries map[sizeKey]*sizeEntry
	workers int
	// walk is overridden by tests to control completion without sleeps.
	walk func(context.Context, string) (int64, bool, error)
}

func (c *SizeCache) Snapshot(rows []Candidate) []Candidate {
	out := append([]Candidate(nil), rows...)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[sizeKey]*sizeEntry)
	}
	live := make(map[sizeKey]bool)
	now := time.Now()
	for i := range out {
		row := &out[i]
		if row.Status != "ready" {
			continue
		}
		row.Size, row.SizeKnown, row.SizeCapped = 0, false, false
		info, err := os.Lstat(row.Path)
		if err != nil {
			continue
		}
		key := sizeKey{row.ID, row.Path, info.ModTime().UnixNano()}
		live[key] = true
		entry := c.entries[key]
		if entry == nil {
			entry = &sizeEntry{pending: true}
			c.entries[key] = entry
		} else if !entry.pending && !entry.running && now.Sub(entry.at) >= sizeRefreshInterval {
			entry.pending, entry.known = true, false
		}
		row.Size, row.SizeKnown, row.SizeCapped = entry.size, entry.known, entry.capped
	}
	// Removal also cancels in-flight work. Its completion cannot resurrect a row.
	for key, entry := range c.entries {
		if !live[key] {
			if entry.cancel != nil {
				entry.cancel()
			}
			delete(c.entries, key)
		}
	}
	pending := 0
	for _, entry := range c.entries {
		if entry.pending {
			pending++
		}
	}
	for c.workers < 2 && pending > 0 {
		c.workers++
		pending--
		go c.work()
	}
	return out
}

func (c *SizeCache) work() {
	for {
		c.mu.Lock()
		var key sizeKey
		var entry *sizeEntry
		for k, e := range c.entries {
			if e.pending {
				key, entry = k, e
				break
			}
		}
		if entry == nil {
			c.workers--
			c.mu.Unlock()
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		entry.pending, entry.running, entry.cancel = false, true, cancel
		walk := c.walk
		if walk == nil {
			walk = WalkSize
		}
		c.mu.Unlock()

		size, capped, err := walk(ctx, key.path)
		cancel()
		c.mu.Lock()
		if c.entries[key] == entry {
			entry.size, entry.capped, entry.known = size, capped, err == nil
			entry.at, entry.running, entry.cancel = time.Now(), false, nil
		}
		c.mu.Unlock()
	}
}
