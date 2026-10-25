package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
	"github.com/labstack/driga/internal/viewer"
)

type updateSource struct {
	version atomic.Int64
	mu      sync.Mutex
	tail    []view.TailEvent
}

func (s *updateSource) DataVersion(context.Context) (int64, error) { return s.version.Load(), nil }
func (s *updateSource) LoadScope(context.Context, store.Scope, ...bool) (viewer.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return viewer.Snapshot{Tail: append([]view.TailEvent(nil), s.tail...)}, nil
}

// setTail replaces the tail and bumps the data version, as a collector commit does.
func (s *updateSource) setTail(rows ...view.TailEvent) {
	s.mu.Lock()
	s.tail = rows
	s.mu.Unlock()
	s.version.Add(1)
}

func watched(t *testing.T, source Source) *Server {
	t.Helper()
	s, err := New(source, Options{})
	if err != nil {
		t.Fatal(err)
	}
	s.watchInterval = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.Watch(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	return s
}

type updateResponse struct {
	Cursor  string           `json:"cursor"`
	Refresh bool             `json:"refresh"`
	Tail    []view.TailEvent `json:"tail"`
}

func poll(t *testing.T, s *Server, after string) updateResponse {
	t.Helper()
	target := "/updates"
	if after != "" {
		target += "?after=" + url.QueryEscape(after)
	}
	w := request(s, "GET", target)
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("updates: %d %v %s", w.Code, w.Header(), w.Body)
	}
	var out updateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Cursor == "" || out.Tail == nil {
		t.Fatalf("updates body %s: %v", w.Body, err)
	}
	return out
}

func TestUpdatesReturnsCursorAtOnce(t *testing.T) {
	s := watched(t, &updateSource{})
	s.pollTimeout = time.Hour
	start := time.Now()
	got := poll(t, s, "")
	if got.Refresh || time.Since(start) > time.Second {
		t.Fatalf("first call: %+v after %v", got, time.Since(start))
	}
}

func TestUpdatesWakesOnDataVersion(t *testing.T) {
	source := &updateSource{}
	s := watched(t, source)
	s.pollTimeout = time.Hour
	s.refreshInterval = time.Hour
	first := poll(t, s, "")
	time.AfterFunc(100*time.Millisecond, func() { source.version.Add(1) })
	done := make(chan updateResponse, 1)
	go func() { done <- poll(t, s, first.Cursor) }()
	select {
	case got := <-done:
		if !got.Refresh || got.Cursor == first.Cursor {
			t.Fatalf("no refresh: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("long poll did not wake on a data version change")
	}
}

func TestUpdatesTimesOut(t *testing.T) {
	s := watched(t, &updateSource{})
	s.refreshInterval = time.Hour
	first := poll(t, s, "")
	s.pollTimeout = 50 * time.Millisecond
	start := time.Now()
	got := poll(t, s, first.Cursor)
	if got.Refresh || len(got.Tail) != 0 || time.Since(start) > time.Second {
		t.Fatalf("timeout: %+v after %v", got, time.Since(start))
	}
	if again := poll(t, s, got.Cursor); again.Refresh {
		t.Fatal("a timed-out cursor reported a refresh")
	}
}

func TestUpdatesForcedRefresh(t *testing.T) {
	s := watched(t, &updateSource{})
	s.pollTimeout = time.Hour
	s.refreshInterval = 30 * time.Millisecond
	first := poll(t, s, "")
	if got := poll(t, s, first.Cursor); !got.Refresh {
		t.Fatal("no forced refresh without commits")
	}
}

func TestUpdatesNoDuplicateTail(t *testing.T) {
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	row := func(key string, offset int) view.TailEvent {
		return view.TailEvent{Key: key, TS: at.Add(time.Duration(offset) * time.Second), Summary: key}
	}
	source := &updateSource{}
	source.setTail(row("a", 0))
	s := watched(t, source)
	s.pollTimeout = 50 * time.Millisecond
	s.refreshInterval = time.Hour
	seen := map[string]bool{}
	check := func(got updateResponse) {
		t.Helper()
		for _, r := range got.Tail {
			if seen[r.Key] {
				t.Fatalf("tail key %q repeated", r.Key)
			}
			seen[r.Key] = true
		}
	}
	got := poll(t, s, "")
	check(got)
	if !seen["a"] {
		t.Fatal("first call omitted the current tail")
	}
	source.setTail(row("a", 0), row("b", 1), row("c", 1))
	got = poll(t, s, got.Cursor)
	check(got)
	if !seen["b"] || !seen["c"] {
		t.Fatalf("new rows missing: %+v", got)
	}
	for range 3 {
		got = poll(t, s, got.Cursor)
		check(got)
	}
	source.setTail(row("a", 0), row("b", 1), row("c", 1), row("d", 2))
	check(poll(t, s, got.Cursor))
	if !seen["d"] {
		t.Fatal("later row missing")
	}
}

// A row that lands late, older than one the page holds, still reaches the
// page once, as from a machine whose events sync after another's.
func TestUpdatesLateTailRow(t *testing.T) {
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	row := func(key string, offset int) view.TailEvent {
		return view.TailEvent{Key: key, TS: at.Add(time.Duration(offset) * time.Second), Summary: key}
	}
	source := &updateSource{}
	source.setTail(row("a", 0), row("c", 5))
	s := watched(t, source)
	s.pollTimeout = 50 * time.Millisecond
	s.refreshInterval = time.Hour
	got := poll(t, s, "")
	source.setTail(row("a", 0), row("b", 2), row("c", 5))
	got = poll(t, s, got.Cursor)
	if len(got.Tail) != 1 || got.Tail[0].Key != "b" {
		t.Fatalf("late row not sent: %+v", got.Tail)
	}
	if again := poll(t, s, got.Cursor); len(again.Tail) != 0 {
		t.Fatalf("late row sent twice: %+v", again.Tail)
	}
}

// The remembered keys stay bounded; rows older than the oldest remembered
// one count as seen.
func TestUpdatesSeenKeysBounded(t *testing.T) {
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	rows := []view.TailEvent{}
	for i := range 3 * seenKeys {
		rows = append(rows, view.TailEvent{Key: fmt.Sprintf("%064x", i), TS: at.Add(time.Duration(i) * time.Second)})
	}
	source := &updateSource{}
	source.setTail(rows...)
	s := watched(t, source)
	s.pollTimeout = 50 * time.Millisecond
	s.refreshInterval = time.Hour
	got := poll(t, s, "")
	if len(got.Cursor) > 64*seenKeys {
		t.Fatalf("cursor grew to %d bytes", len(got.Cursor))
	}
	if again := poll(t, s, got.Cursor); len(again.Tail) != 0 {
		t.Fatalf("old rows resent: %d", len(again.Tail))
	}
}

func TestUpdatesTailFloodBound(t *testing.T) {
	source := &updateSource{}
	rows := []view.TailEvent{}
	at := time.Now()
	for i := range 60 {
		rows = append(rows, view.TailEvent{Key: string(rune('A' + i)), TS: at.Add(time.Duration(i) * time.Millisecond)})
	}
	source.setTail(rows...)
	s := watched(t, source)
	got := poll(t, s, "")
	if len(got.Tail) != 20 || got.Tail[19].Key != rows[59].Key {
		t.Fatalf("flood policy keeps the newest twenty: %d", len(got.Tail))
	}
}

func TestUpdatesTailWithoutVersionChange(t *testing.T) {
	source := &updateSource{}
	source.setTail(view.TailEvent{Key: "event", Summary: "arrived"})
	s := watched(t, source)
	s.pollTimeout = 2 * time.Second
	s.refreshInterval = time.Hour
	first := poll(t, s, "")
	// Replace the cached tail without a data version change: the watch tick
	// must still discover the new key for a waiting poll.
	s.cacheMu.Lock()
	for key, cached := range s.cache {
		cached.snap.Tail = []view.TailEvent{{Key: "next", Summary: "arrived", TS: time.Now()}}
		s.cache[key] = cached
	}
	s.cacheMu.Unlock()
	got := poll(t, s, first.Cursor)
	if got.Refresh || len(got.Tail) != 1 || got.Tail[0].Key != "next" {
		t.Fatalf("tail tick: %+v", got)
	}
}

func TestUpdatesTrackBrowserActivity(t *testing.T) {
	activity := make(chan bool, 100)
	s, err := New(&updateSource{}, Options{Activity: func(active bool) { activity <- active }})
	if err != nil {
		t.Fatal(err)
	}
	s.watchInterval, s.refreshInterval, s.pollTimeout = 10*time.Millisecond, time.Hour, time.Hour
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
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
	first := poll(t, s, "")
	pollCtx, stop := context.WithCancel(ctx)
	r := httptest.NewRequest("GET", "/updates?after="+url.QueryEscape(first.Cursor), nil).WithContext(pollCtx)
	polled := make(chan struct{})
	go func() { defer close(polled); s.ServeHTTP(httptest.NewRecorder(), r) }()
	waitActivity(true)
	stop()
	<-polled
	waitActivity(false)
}

func TestUpdatesRejectBadCursor(t *testing.T) {
	s := watched(t, &updateSource{})
	if w := request(s, "GET", "/updates?after=nope"); w.Code != 400 {
		t.Fatal(w.Code)
	}
}

func TestUpdatesRecoverAfterReadError(t *testing.T) {
	original, st, _, _ := fixture(t)
	s := watched(t, &initiallyUnavailable{Source: original.source})
	s.pollTimeout = 3 * time.Second
	s.refreshInterval = time.Hour
	first := poll(t, s, "")
	if err := st.SetMeta("recovered", "yes"); err != nil {
		t.Fatal(err)
	}
	if got := poll(t, s, first.Cursor); !got.Refresh {
		t.Fatal("watcher stopped after a transient error")
	}
}

func TestNoAuthOrHostChecks(t *testing.T) {
	s, _, _, _ := fixture(t)
	for _, host := range []string{"evil.test", "mygo.localhost", "127.0.0.1:1"} {
		r := httptest.NewRequest("GET", "http://"+host+"/", nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 200 || len(w.Result().Cookies()) != 0 {
			t.Errorf("%s: %d %v", host, w.Code, w.Result().Cookies())
		}
	}
	if w := request(s, "GET", "/?token=anything"); w.Code != 200 {
		t.Errorf("token query: %d", w.Code)
	}
}

func TestScriptsHaveNoEventSource(t *testing.T) {
	scripts, err := fs.Glob(assets, "static/*.js")
	if err != nil || len(scripts) == 0 {
		t.Fatal(scripts, err)
	}
	templates, err := fs.Glob(assets, "templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range append(scripts, templates...) {
		raw, err := assets.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, banned := range []string{"EventSource", "X-CSRF-Token", "chat-csrf", "driga-theme", "credentials:", "/events"} {
			if strings.Contains(string(raw), banned) {
				t.Errorf("%s contains %q", name, banned)
			}
		}
	}
	if _, err := assets.ReadFile("static/vendor/htmx-ext-sse-2.2.2.js"); err == nil {
		t.Error("unused SSE extension still embedded")
	}
	if w := request(newTestServer(t), "GET", "/events"); w.Code != 404 {
		t.Errorf("/events: %d", w.Code)
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	s, _, _, _ := fixture(t)
	return s
}

func postJSON(s *Server, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestAppConfirmNotFoundWithoutHook(t *testing.T) {
	s, err := New(&updateSource{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if w := postJSON(s, "/app/confirm", `{"message":"Stop?"}`); w.Code != 404 {
		t.Errorf("confirm: %d", w.Code)
	}
	if w := postJSON(s, "/app/open-external", `{"url":"https://driga.dev"}`); w.Code != 404 {
		t.Errorf("open-external: %d", w.Code)
	}
}

func TestAppHooks(t *testing.T) {
	var asked, opened string
	answer := true
	s, err := New(&updateSource{}, Options{
		Confirm:      func(_ context.Context, message string) bool { asked = message; return answer },
		OpenExternal: func(url string) { opened = url },
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []bool{true, false} {
		answer = want
		w := postJSON(s, "/app/confirm", `{"message":"Stop session?"}`)
		var out struct{ Confirmed bool }
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Confirmed != want || asked != "Stop session?" {
			t.Fatalf("confirm %v: %d %s", want, w.Code, w.Body)
		}
	}
	if w := postJSON(s, "/app/open-external", `{"url":"https://driga.dev/docs"}`); w.Code != 200 || opened != "https://driga.dev/docs" {
		t.Fatalf("open-external: %d %s %q", w.Code, w.Body, opened)
	}
	opened = ""
	for _, bad := range []string{`{"url":"file:///etc/passwd"}`, `{"url":"javascript:alert(1)"}`, `{"url":"https://x","extra":1}`} {
		if w := postJSON(s, "/app/open-external", bad); w.Code != 400 || opened != "" {
			t.Errorf("%s: %d %q", bad, w.Code, opened)
		}
	}
	r := httptest.NewRequest("POST", "/app/confirm", strings.NewReader(`{"message":"x"}`))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("content type: %d", w.Code)
	}
	if w := request(s, "GET", "/app/confirm"); w.Code == 200 {
		t.Errorf("GET confirm: %d", w.Code)
	}
}
