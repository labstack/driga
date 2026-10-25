package web

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
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
	s, err := New(viewer.NewStoreSource(ro, home, view.Filter{All: true}), Options{CleanupList: func(context.Context) ([]cleanup.Candidate, error) {
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

func request(s *Server, method, target string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestMethods(t *testing.T) {
	s, _, _, _ := fixture(t)
	for _, method := range []string{"POST", "PUT", "DELETE"} {
		w := request(s, method, "/")
		if w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD" {
			t.Errorf("%s: %d", method, w.Code)
		}
	}
	if w := request(s, "HEAD", "/"); w.Code != 200 || w.Body.Len() != 0 {
		t.Errorf("HEAD: %d %s", w.Code, w.Body)
	}
}

func TestHeaders(t *testing.T) {
	s, _, _, _ := fixture(t)
	for _, path := range []string{"/", "/agents", "/static/app.js"} {
		w := request(s, "GET", path)
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
			w := request(s, "GET", path)
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
	r := httptest.NewRequest("GET", "/agents", nil)
	r.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 || strings.Contains(w.Body.String(), "<!DOCTYPE") || !strings.Contains(w.Body.String(), "&lt;script&gt;") {
		t.Fatalf("fragment: %d %s", w.Code, w.Body)
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
		w := request(s, "GET", path)
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
	w := request(s, "GET", "/agents")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Peer work") || strings.Contains(w.Body.String(), "&lt;script&gt;") {
		t.Fatalf("default scope: %d %s", w.Code, w.Body)
	}
	if w := request(s, "GET", fmt.Sprint("/agents/", localAgent, "?machine=peer")); w.Code != 404 {
		t.Errorf("out-of-scope detail: %d", w.Code)
	}
	if w := request(s, "GET", fmt.Sprint("/agents/", peerAgent, "?machine=peer")); w.Code != 200 {
		t.Errorf("peer detail: %d %s", w.Code, w.Body)
	}
	if w := request(s, "GET", "/?machine=unknown"); w.Code != 400 {
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

func TestMuxRetainsPathAndMethodResponses(t *testing.T) {
	s, _, _, _ := fixture(t)
	for _, tc := range []struct {
		method, path string
		status       int
		allow        string
	}{
		{"GET", "/static", 404, ""},
		{"GET", "/chat/api", 404, ""},
		{"GET", "/missing//page", 404, ""},
		{"GET", "/chat/api/../unknown", 503, ""},
		{"GET", "/flow.json/", 404, ""},
		{"GET", "/static%2ftheme.css", 200, ""},
		{"GET", "/chat/api/send", 405, "POST"},
		{"DELETE", "/chat/api/send", 405, "GET, HEAD"},
	} {
		w := request(s, tc.method, tc.path)
		if w.Code != tc.status || w.Header().Get("Allow") != tc.allow {
			t.Errorf("%s %s: status=%d Allow=%q", tc.method, tc.path, w.Code, w.Header().Get("Allow"))
		}
	}
}
