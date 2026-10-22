package web

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
	"github.com/labstack/driga/internal/viewer"
)

const title = "<script>alert(1)</script>"

func fixture(t *testing.T) (*Server, *store.Store, int64, int64) {
	t.Helper()
	home := t.TempDir()
	for _, key := range []string{"HOME", "XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "DRIGA_DATA_DIR", "DRIGA_SPOOL_DIR", "DRIGA_CONFIG", "CODEX_HOME", "GEMINI_CLI_HOME"} {
		t.Setenv(key, filepath.Join(home, key))
	}
	path := filepath.Join(home, "driga.db")
	st, err := store.Open(path, store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	var agent, project int64
	err = st.Tx(context.Background(), func(tx *store.Tx) error {
		var err error
		project, err = tx.UpsertRepo(model.Repo{Name: "Fixture project", Path: "/fixture"})
		if err != nil {
			return err
		}
		if err := tx.UpsertMachine(model.Machine{OS: "linux", Hostname: "Fixture machine", Cores: 4, MemTotal: 8 << 30}); err != nil {
			return err
		}
		if err := tx.InsertSample(model.MachineSample{TS: time.Now(), MemTotal: 8 << 30, MemUsed: 4 << 30, SwapTotal: 1 << 30, SwapUsed: 1 << 20, DiskFree: 64 << 30, Cores: 4, Load1: 1.5}); err != nil {
			return err
		}
		if _, err := tx.UpsertWorktree(model.Worktree{RepoID: project, Path: "/fixture", Branch: "main", Dirty: 1, Ahead: 2}); err != nil {
			return err
		}
		if err := tx.InsertEvent(model.Event{Adapter: "claude", ExternalID: "fixture", TS: time.Now(), Kind: "Stop", Summary: title}); err != nil {
			return err
		}
		if err := tx.ReplaceOrphans([]model.Orphan{{ID: "42:1", RootPID: 42, Signature: title, Cwd: "/fixture", RSSTotal: 1024, ProcCount: 1, Evidence: []string{title}}}); err != nil {
			return err
		}
		agent, err = tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "fixture", Cwd: "/fixture", Title: title, LastActivityAt: time.Now()}, RepoID: &project, Label: "waiting"})
		if err != nil {
			return err
		}
		return tx.UpsertInboxItem(model.InboxItem{ID: "waiting", SessionID: agent, Kind: "waiting", Summary: title, TS: time.Now()})
	})
	if err != nil {
		t.Fatal(err)
	}
	ro, err := store.Open(path, store.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ro.Close() })
	s, err := New(viewer.NewStoreSource(ro, home, view.Filter{All: true}), Options{Host: "127.0.0.1:4321", CleanupList: func(context.Context) ([]cleanup.Candidate, error) {
		return []cleanup.Candidate{{ID: "wt:fixture", Kind: cleanup.Worktree, Path: "/fixture/old", Status: "refused", Evidence: []string{title}}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.Watch(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	return s, st, agent, project
}

func request(s *Server, method, target, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:4321"+target, nil)
	if token != "" {
		r.AddCookie(&http.Cookie{Name: s.cookieName, Value: token})
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestAuthentication(t *testing.T) {
	s, _, _, _ := fixture(t)
	for _, path := range []string{"/", "/agents", "/events", "/static/app.js"} {
		if w := request(s, "GET", path, ""); w.Code != http.StatusUnauthorized {
			t.Errorf("%s: %d", path, w.Code)
		}
	}
	if w := request(s, "GET", "/?token=wrong", ""); w.Code != 401 {
		t.Errorf("bad token: %d", w.Code)
	}
	w := request(s, "GET", "/agents?machine=local&token="+s.token, "")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/agents?machine=local" {
		t.Fatalf("redirect: %d %v", w.Code, w.Header())
	}
	cs := w.Result().Cookies()
	if len(cs) != 1 || cs[0].Value != s.token || !cs[0].HttpOnly || cs[0].SameSite != http.SameSiteStrictMode || cs[0].Path != "/" {
		t.Fatalf("cookie: %+v", cs)
	}
}

func TestHostAndMethods(t *testing.T) {
	s, _, _, _ := fixture(t)
	r := httptest.NewRequest("GET", "http://evil.test/", nil)
	r.AddCookie(&http.Cookie{Name: s.cookieName, Value: s.token})
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("host: %d", w.Code)
	}
	for _, host := range []string{"localhost:4321", "127.0.0.1:4321"} {
		r.Host = host
		w = httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Errorf("allowed host %s: %d", host, w.Code)
		}
	}
	for _, method := range []string{"POST", "PUT", "DELETE"} {
		w := request(s, method, "/", s.token)
		if w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD" {
			t.Errorf("%s: %d", method, w.Code)
		}
	}
	if w := request(s, "HEAD", "/", s.token); w.Code != 200 || w.Body.Len() != 0 {
		t.Errorf("HEAD: %d %s", w.Code, w.Body)
	}
}

func TestHeaders(t *testing.T) {
	s, _, _, _ := fixture(t)
	for _, path := range []string{"/", "/agents", "/static/app.js"} {
		w := request(s, "GET", path, s.token)
		for key, value := range map[string]string{
			"Content-Security-Policy": "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'",
			"X-Content-Type-Options":  "nosniff", "Referrer-Policy": "no-referrer", "Cache-Control": "no-store",
		} {
			if w.Header().Get(key) != value {
				t.Errorf("%s %s: %q", path, key, w.Header().Get(key))
			}
		}
	}
}

func TestPagesAndEscaping(t *testing.T) {
	s, _, agent, project := fixture(t)
	for _, path := range []string{"/", "/agents", fmt.Sprint("/agents/", agent), "/projects", fmt.Sprint("/projects/", project), "/inbox", "/orphans", "/machines", "/cleanup"} {
		t.Run(path, func(t *testing.T) {
			w := request(s, "GET", path, s.token)
			if w.Code != 200 || !strings.Contains(w.Body.String(), "<!DOCTYPE html>") {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			if strings.Contains(w.Body.String(), title) {
				t.Fatal("unescaped stored script")
			}
			if path == "/" || path == "/agents" || path == fmt.Sprint("/agents/", agent) {
				if !strings.Contains(w.Body.String(), "&lt;script&gt;alert(1)&lt;/script&gt;") {
					t.Fatal("escaped title missing")
				}
			}
		})
	}
	r := httptest.NewRequest("GET", "http://127.0.0.1:4321/agents", nil)
	r.Header.Set("HX-Request", "true")
	r.AddCookie(&http.Cookie{Name: s.cookieName, Value: s.token})
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 || strings.Contains(w.Body.String(), "<!DOCTYPE") || !strings.Contains(w.Body.String(), "&lt;script&gt;") {
		t.Fatalf("fragment: %d %s", w.Code, w.Body)
	}
}

func TestSSEDataChange(t *testing.T) {
	s, st, _, _ := fixture(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { r.Host = "127.0.0.1:4321"; s.ServeHTTP(w, r) }))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/events", nil)
	r.AddCookie(&http.Cookie{Name: s.cookieName, Value: s.token})
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("SSE: %d", resp.StatusCode)
	}
	if err := st.SetMeta("change", "yes"); err != nil {
		t.Fatal(err)
	}
	scan := bufio.NewScanner(resp.Body)
	refreshed := false
	for scan.Scan() {
		if scan.Text() == "event: refresh" {
			refreshed = true
		}
		if scan.Text() == "event: change" {
			if !refreshed {
				t.Fatal("missing refresh event for JSON flow clients")
			}
			return
		}
	}
	t.Fatalf("no change event: %v", scan.Err())
}

func TestTokenEntropy(t *testing.T) {
	s, _, _, _ := fixture(t)
	decoded, err := url.QueryUnescape(s.token)
	if err != nil || len(decoded) < 43 {
		t.Fatalf("token: %q %v", s.token, err)
	}
}

func TestRecentFinishedSessionWithAllOnHomeAndAgents(t *testing.T) {
	s, st, _, project := fixture(t)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "finished", Title: "Recently finished", LastActivityAt: time.Now()}, RepoID: &project, Label: "finished", EndedAt: timePointer(time.Now())})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/?all=1", "/agents?all=1"} {
		w := request(s, "GET", path, s.token)
		if !strings.Contains(w.Body.String(), "Recently finished") {
			t.Errorf("%s lacks recent completion", path)
		}
	}
}

func timePointer(t time.Time) *time.Time { return &t }

func TestMachineScope(t *testing.T) {
	s, st, localAgent, _ := fixture(t)
	var peerAgent int64
	err := st.Tx(context.Background(), func(tx *store.Tx) error {
		peer := tx.ForMachine("peer")
		if err := peer.UpsertMachine(model.Machine{Name: "Workshop", Hostname: "worker", DeviceID: "ABCDEF123456", State: "live", LastContactAt: time.Now()}); err != nil {
			return err
		}
		var err error
		peerAgent, err = peer.UpsertSession(model.Session{SessionObs: model.SessionObs{ExternalID: "peer-session", Adapter: "codex", Title: "Peer work", LastActivityAt: time.Now()}, Label: "working"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	s.opts.Machine = "abcde"
	w := request(s, "GET", "/agents", s.token)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Peer work") || strings.Contains(w.Body.String(), "&lt;script&gt;") {
		t.Fatalf("default scope: %d %s", w.Code, w.Body)
	}
	if w := request(s, "GET", fmt.Sprint("/agents/", localAgent, "?machine=peer"), s.token); w.Code != 404 {
		t.Errorf("out-of-scope detail: %d", w.Code)
	}
	if w := request(s, "GET", fmt.Sprint("/agents/", peerAgent, "?machine=peer"), s.token); w.Code != 200 {
		t.Errorf("peer detail: %d %s", w.Code, w.Body)
	}
	if w := request(s, "GET", "/?machine=unknown", s.token); w.Code != 400 {
		t.Errorf("unknown scope: %d", w.Code)
	}
}

type initiallyUnavailable struct {
	Source
	seen atomic.Bool
}

func (s *initiallyUnavailable) DataVersion(ctx context.Context) (int64, error) {
	if !s.seen.Swap(true) {
		return 0, errors.New("temporarily unavailable")
	}
	return s.Source.DataVersion(ctx)
}

func TestSSERecoversAfterReadError(t *testing.T) {
	original, st, _, _ := fixture(t)
	s, err := New(&initiallyUnavailable{Source: original.source}, original.opts)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); s.Watch(ctx) }()
	defer func() { cancel(); <-done }()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { r.Host = s.opts.Host; s.ServeHTTP(w, r) }))
	defer ts.Close()
	r, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/events", nil)
	r.AddCookie(&http.Cookie{Name: s.cookieName, Value: s.token})
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if err := st.SetMeta("recovered", "yes"); err != nil {
		t.Fatal(err)
	}
	scan := bufio.NewScanner(resp.Body)
	for scan.Scan() {
		if scan.Text() == "event: change" {
			return
		}
	}
	t.Fatalf("watcher stopped after a transient error: %v", scan.Err())
}
