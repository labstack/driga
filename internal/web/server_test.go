package web

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
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
	if w := request(s, "HEAD", "/app/"); w.Code != 200 || w.Body.Len() != 0 {
		t.Errorf("HEAD: %d %s", w.Code, w.Body)
	}
}

func TestHeaders(t *testing.T) {
	s, _, _, _ := fixture(t)
	for _, path := range []string{"/", "/missing", "/static/fonts/ibm-plex-sans-latin-400-normal.woff2"} {
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

// The app's page allows Mantine's style element by a nonce that is new on
// every response and matches the one the page hands to Mantine.
func TestAppPageStyleNonce(t *testing.T) {
	s, _, _, _ := fixture(t)
	nonce := regexp.MustCompile(`style-src 'self' 'nonce-([A-Za-z0-9+/=]{24})'`)
	var seen []string
	for range 2 {
		w := request(s, "GET", "/app/")
		m := nonce.FindStringSubmatch(w.Header().Get("Content-Security-Policy"))
		if w.Code != 200 || m == nil || !strings.Contains(w.Body.String(), `name="driga-style-nonce" content="`+m[1]+`"`) || strings.Contains(w.Body.String(), "__DRIGA_STYLE_NONCE__") {
			t.Fatalf("app page: %d %q", w.Code, w.Header().Get("Content-Security-Policy"))
		}
		if strings.Replace(w.Header().Get("Content-Security-Policy"), " 'nonce-"+m[1]+"'", "", 1) != csp {
			t.Errorf("policy beyond the nonce: %q", w.Header().Get("Content-Security-Policy"))
		}
		seen = append(seen, m[1])
	}
	if seen[0] == seen[1] {
		t.Error("the nonce repeats")
	}
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
		{"GET", "/static%2ffonts/ibm-plex-mono-latin-400-normal.woff2", 200, ""},
		{"GET", "/chat/api/send", 405, "POST"},
		{"DELETE", "/chat/api/send", 405, "GET, HEAD"},
	} {
		w := request(s, tc.method, tc.path)
		if w.Code != tc.status || w.Header().Get("Allow") != tc.allow {
			t.Errorf("%s %s: status=%d Allow=%q", tc.method, tc.path, w.Code, w.Header().Get("Allow"))
		}
	}
}
