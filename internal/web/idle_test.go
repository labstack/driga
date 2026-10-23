package web

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
	"github.com/labstack/driga/internal/viewer"
)

type tailSource struct{ version atomic.Int64 }

func (s *tailSource) DataVersion(context.Context) (int64, error) { return s.version.Load(), nil }
func (s *tailSource) LoadScope(context.Context, store.Scope, ...bool) (viewer.Snapshot, error) {
	return viewer.Snapshot{Tail: []view.TailEvent{{Key: "event", Summary: "arrived"}}}, nil
}

func TestDashboardSharesTailTicksAndTracksBrowserActivity(t *testing.T) {
	source := &tailSource{}
	activity := make(chan bool, 100)
	s, err := New(source, Options{Host: "127.0.0.1:4321", Activity: func(active bool) { activity <- active }})
	if err != nil {
		t.Fatal(err)
	}
	s.watchInterval, s.refreshInterval = 10*time.Millisecond, time.Hour
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); s.Watch(ctx) }()
	defer func() { cancel(); <-done }()
	waitActivity := func(want bool) {
		t.Helper()
		for {
			select {
			case got := <-activity:
				if got == want {
					return
				}
			case <-ctx.Done():
				t.Fatal("browser activity did not change", want)
			}
		}
	}
	waitActivity(false)
	ts := httptest.NewServer(http.HandlerFunc(s.events))
	defer ts.Close()
	responses := []*http.Response{}
	for range 2 {
		req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/events", nil)
		res, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		responses = append(responses, res)
		defer res.Body.Close()
	}
	waitActivity(true)
	// Replace the cached tail without a data-version change. Each client's
	// seen set is unchanged; the shared scheduler must discover the new key.
	s.cacheMu.Lock()
	for key, cached := range s.cache {
		cached.snap.Tail = []view.TailEvent{{Key: "next", Summary: "arrived"}}
		s.cache[key] = cached
	}
	s.cacheMu.Unlock()
	for _, res := range responses {
		scan := bufio.NewScanner(res.Body)
		found := false
		for scan.Scan() {
			if strings.HasPrefix(scan.Text(), "data: ") && strings.Contains(scan.Text(), `"next"`) {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("shared tick did not deliver tail without a commit", scan.Err())
		}
		res.Body.Close()
	}
	waitActivity(false)
}
