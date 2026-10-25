package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/viewer"
)

type appSource struct{}

func (appSource) DataVersion(context.Context) (int64, error) { return 1, nil }
func (appSource) LoadScope(context.Context, store.Scope, ...bool) (viewer.Snapshot, error) {
	return viewer.Snapshot{}, nil
}

func postJSON(s *Server, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestAppConfirmNotFoundWithoutHook(t *testing.T) {
	s, err := New(appSource{}, Options{})
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
	s, err := New(appSource{}, Options{
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

func TestRootOpensTheApp(t *testing.T) {
	s, err := New(appSource{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if w := request(s, "GET", "/"); w.Code != http.StatusFound || w.Header().Get("Location") != "/app/" {
		t.Fatalf("/: %d %q", w.Code, w.Header().Get("Location"))
	}
	for _, gone := range []string{"/flow", "/machines", "/inbox", "/updates", "/flow.json", "/static/app.js"} {
		if w := request(s, "GET", gone); w.Code != http.StatusNotFound {
			t.Errorf("%s: %d", gone, w.Code)
		}
	}
	if w := request(s, "GET", "/static/fonts/ibm-plex-sans-latin-400-normal.woff2"); w.Code != 200 {
		t.Errorf("font: %d", w.Code)
	}
}

// The app counts as open while it keeps asking, and closed soon after.
func TestWatchReportsAppActivity(t *testing.T) {
	activity := make(chan bool, 100)
	s, err := New(appSource{}, Options{Activity: func(active bool) { activity <- active }})
	if err != nil {
		t.Fatal(err)
	}
	s.watchInterval = 5 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Watch(ctx)
	if <-activity {
		t.Fatal("active before the app asked")
	}
	wait := func(want bool) {
		t.Helper()
		deadline := time.After(time.Second)
		for {
			select {
			case active := <-activity:
				if active == want {
					return
				}
			case <-deadline:
				t.Fatalf("never reported active = %v", want)
			}
		}
	}
	postJSON(s, "/app/api/list", `{}`)
	wait(true)
	s.mu.Lock()
	s.polled = time.Now().Add(-activeFor)
	s.mu.Unlock()
	wait(false)
}
