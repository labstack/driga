package web

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/viewer"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStaticErrorsKeepHeaders(t *testing.T) {
	s, _, _, _ := fixture(t)
	for _, tc := range []struct {
		path, rangeHeader string
		code              int
	}{{"/static/missing", "", 404}, {"/static/app.js", "bytes=999999999-", 416}} {
		r := httptest.NewRequest("GET", "http://"+s.opts.Host+tc.path, nil)
		r.AddCookie(&http.Cookie{Name: s.cookieName, Value: s.token})
		r.Header.Set("Range", tc.rangeHeader)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != tc.code || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Security-Policy") != csp {
			t.Errorf("%s: %d %v", tc.path, w.Code, w.Header())
		}
	}
}
func TestCookiesDistinctAcrossPorts(t *testing.T) {
	s, _, _, _ := fixture(t)
	other, err := New(s.source, Options{Host: "127.0.0.1:4322"})
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, srv := range []*Server{s, other} {
		r := httptest.NewRequest("GET", srv.URL(), nil)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)
		cs := w.Result().Cookies()
		if len(cs) != 1 {
			t.Fatal(cs)
		}
		names[cs[0].Name] = true
	}
	if len(names) != 2 {
		t.Fatalf("cookies collide: %v", names)
	}
}

type countingSource struct {
	Source
	loads int
}

func (s *countingSource) LoadScope(ctx context.Context, scope store.Scope, all ...bool) (viewer.Snapshot, error) {
	s.loads++
	return s.Source.LoadScope(ctx, scope, all...)
}
func TestOverviewAndCleanupCaches(t *testing.T) {
	base, _, _, _ := fixture(t)
	source := &countingSource{Source: base.source}
	s, err := New(source, base.opts)
	if err != nil {
		t.Fatal(err)
	}
	cleanups := 0
	s.opts.CleanupList = func(context.Context) ([]cleanup.Candidate, error) { cleanups++; return nil, nil }
	for i := 0; i < 2; i++ {
		p, code, err := s.loadPage(httptest.NewRequest("GET", "http://"+s.opts.Host+"/", nil))
		if err != nil || code != 200 {
			t.Fatal(code, err)
		}
		if len(p.AgentDetails) != 0 {
			t.Fatal("Home built agent details")
		}
	}
	if source.loads != 1 || cleanups != 1 {
		t.Fatalf("loads=%d cleanups=%d", source.loads, cleanups)
	}
}
func TestSameNamedProjectsStaySeparate(t *testing.T) {
	s, st, _, _ := fixture(t)
	err := st.Tx(context.Background(), func(tx *store.Tx) error {
		for i, path := range []string{"/a/app", "/b/app"} {
			id, err := tx.UpsertRepo(model.Repo{Name: "app", Path: path})
			if err != nil {
				return err
			}
			_, err = tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: path, Branch: fmt.Sprint("branch", i), Title: fmt.Sprint("title", i), LastActivityAt: time.Now()}, RepoID: &id, Label: "working"})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := s.loadPage(httptest.NewRequest("GET", "http://"+s.opts.Host+"/", nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, card := range p.ProjectCards {
		if card.Status.Repo.Name == "app" {
			want := "branch0"
			if card.Status.Repo.Path == "/b/app" {
				want = "branch1"
			}
			if card.Branch != want || len(card.States) != 1 || card.States[0].Count != 1 {
				t.Fatalf("mixed: %+v", card)
			}
		}
	}
}
func TestProjectDetailAll(t *testing.T) {
	s, st, _, project := fixture(t)
	err := st.Tx(context.Background(), func(tx *store.Tx) error {
		_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: "hosted", Title: "Hosted old work", Hosted: true, LastActivityAt: time.Now().Add(-2 * time.Hour)}, RepoID: &project, Label: "idle"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, all := range []bool{false, true} {
		path := fmt.Sprint("/projects/", project)
		if all {
			path += "?all=1"
		}
		w := request(s, "GET", path, s.token)
		if w.Code != 200 || strings.Contains(w.Body.String(), "Hosted old work") != all {
			t.Fatalf("all=%v: %d %s", all, w.Code, w.Body)
		}
	}
}
func TestAttentionOverflowDestination(t *testing.T) {
	s, st, _, _ := fixture(t)
	err := st.Tx(context.Background(), func(tx *store.Tx) error {
		for i := 0; i < 10; i++ {
			_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: fmt.Sprint(i), Title: fmt.Sprint("Needs review ", i), LastActivityAt: time.Now()}, Label: "waiting"})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	w := request(s, "GET", "/", s.token)
	if !strings.Contains(w.Body.String(), "attention=1") {
		t.Fatal("overflow does not link to attention")
	}
	p, _, err := s.loadPage(httptest.NewRequest("GET", "http://"+s.opts.Host+"/inbox?attention=1", nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Needs) != 11 {
		t.Fatalf("destination has %d needs; want 11", len(p.Needs))
	}
}

// pipeResponseWriter writes to a peer that deliberately never reads. The
// controller must set a deadline before the first write to release the handler.
type pipeResponseWriter struct {
	net.Conn
	header http.Header
}

func (w *pipeResponseWriter) Header() http.Header { return w.header }
func (w *pipeResponseWriter) WriteHeader(int)     {}
func (w *pipeResponseWriter) FlushError() error   { return nil }
func TestSSESlowReaderDeadline(t *testing.T) {
	original, _, _, _ := fixture(t)
	s, err := New(original.source, original.opts)
	if err != nil {
		t.Fatal(err)
	}
	s.sseWriteTimeout = 30 * time.Millisecond
	close(s.ready)
	writer, reader := net.Pipe()
	defer writer.Close()
	defer reader.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest("GET", "/events", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() { defer close(done); s.events(&pipeResponseWriter{Conn: writer, header: http.Header{}}, req) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		writer.Close()
		<-done
		t.Fatal("blocked client kept SSE handler alive")
	}
	s.mu.Lock()
	clients := len(s.clients)
	s.mu.Unlock()
	if clients != 0 {
		t.Fatalf("leaked client: %d", clients)
	}
}

type errorFlusher struct{ *httptest.ResponseRecorder }

func (*errorFlusher) FlushError() error { return errors.New("client disconnected") }
func TestSSEDropsFlushFailure(t *testing.T) {
	original, _, _, _ := fixture(t)
	s, err := New(original.source, original.opts)
	if err != nil {
		t.Fatal(err)
	}
	close(s.ready)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.events(&errorFlusher{httptest.NewRecorder()}, httptest.NewRequest("GET", "/events", nil).WithContext(ctx))
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		cancel()
		<-done
		t.Fatal("flush failure did not drop client")
	}
}

func TestSSETicksWithoutCommits(t *testing.T) {
	original, _, _, _ := fixture(t)
	s, err := New(original.source, original.opts)
	if err != nil {
		t.Fatal(err)
	}
	s.watchInterval = 5 * time.Millisecond
	s.refreshInterval = 20 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	watchDone := make(chan struct{})
	go func() { defer close(watchDone); s.Watch(ctx) }()
	defer func() { cancel(); <-watchDone }()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.events(w, r) }))
	defer ts.Close()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	scan := bufio.NewScanner(resp.Body)
	for scan.Scan() {
		if scan.Text() == "event: change" {
			return
		}
	}
	t.Fatalf("no idle refresh: %v", scan.Err())
}

func TestBranchColumnDoesNotWrap(t *testing.T) {
	s, st, agent, _ := fixture(t)
	err := st.Tx(context.Background(), func(tx *store.Tx) error {
		session, err := tx.Session(agent)
		if err != nil {
			return err
		}
		session.Branch = "feature/a-long-branch-name-with-many-segments"
		_, err = tx.UpsertSession(session)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	body := request(s, "GET", "/projects", s.token).Body.String()
	if !strings.Contains(body, `class="mono branch" title="feature/a-long-branch-name-with-many-segments"`) {
		t.Fatal("branch lacks full title")
	}
	css := request(s, "GET", "/static/app.css", s.token).Body.String()
	if !strings.Contains(css, "td.branch") || !strings.Contains(css, "white-space: nowrap") || !strings.Contains(css, "text-overflow: ellipsis") {
		t.Fatal("missing truncation styles")
	}
}

func TestOverviewKeepsQuietProjectBranch(t *testing.T) {
	s, st, _, _ := fixture(t)
	err := st.Tx(context.Background(), func(tx *store.Tx) error {
		id, err := tx.UpsertRepo(model.Repo{Name: "Quiet checkout", Path: "/quiet"})
		if err != nil {
			return err
		}
		_, err = tx.UpsertWorktree(model.Worktree{RepoID: id, Path: "/quiet", Branch: "quiet-main"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := s.loadPage(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, card := range p.ProjectCards {
		if card.Status.Repo.Path == "/quiet" && card.Branch != "quiet-main" {
			t.Fatalf("quiet checkout lost branch: %+v", card)
		}
	}
}
