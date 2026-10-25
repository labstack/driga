package web

import (
	"context"
	"fmt"
	"io/fs"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

func TestDefaultVisibilityAndAll(t *testing.T) {
	s, st, _, project := fixture(t)
	err := st.Tx(context.Background(), func(tx *store.Tx) error {
		for _, ended := range []bool{false, true} {
			session := model.Session{SessionObs: model.SessionObs{Adapter: "codex", ExternalID: fmt.Sprint("hidden-", ended), Hosted: true, Cwd: "/fixture", Title: fmt.Sprint("Hidden session ", ended), LastActivityAt: time.Now().Add(-2 * time.Hour)}, RepoID: &project, Label: "idle", Heuristic: true}
			if ended {
				at := time.Now().Add(-time.Hour)
				session.EndedAt = &at
			}
			if _, err := tx.UpsertSession(session); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/agents", "/projects"} {
		for _, all := range []bool{false, true} {
			query := "?machine=local"
			if all {
				query += "&all=1"
			}
			r := httptest.NewRequest("GET", path+query, nil)
			p, status, err := s.loadPage(r)
			if err != nil || status != 200 {
				t.Fatal(status, err)
			}
			want := 1
			if all {
				want = 3
			}
			if len(p.AgentRows) != want {
				t.Errorf("%s all=%v: got %d agents, want %d", path, all, len(p.AgentRows), want)
			}
			idle := 0
			for _, state := range p.States {
				if state.Label == "idle" {
					idle = state.Count
				}
			}
			if idle != want-1 {
				t.Errorf("%s all=%v: idle count %d, want %d", path, all, idle, want-1)
			}
			projectIdle := 0
			for _, card := range p.ProjectCards {
				for _, state := range card.States {
					if state.Label == "idle" {
						projectIdle += state.Count
					}
				}
			}
			if projectIdle != want-1 {
				t.Errorf("%s all=%v: project idle count %d, want %d", path, all, projectIdle, want-1)
			}

			w := request(s, "GET", path+query)
			body := w.Body.String()
			if !strings.Contains(body, "Show all sessions") && !strings.Contains(body, "Show default sessions") {
				t.Error("missing visibility toggle")
			}
			if all && !strings.Contains(p.RefreshURL, "all=1") {
				t.Error("refresh lost all filter")
			}
		}
	}
}

func TestNeedsYouDedupeCapAndIdentity(t *testing.T) {
	s, st, agent, project := fixture(t)
	now := time.Now()
	err := st.Tx(context.Background(), func(tx *store.Tx) error {
		for i := 0; i < 3; i++ {
			if err := tx.UpsertInboxItem(model.InboxItem{ID: fmt.Sprint("repeat", i), SessionID: agent, Kind: "finished", Summary: "Do not use event summary as title", TS: now.Add(time.Duration(i) * time.Second)}); err != nil {
				return err
			}
		}
		for i := 0; i < 9; i++ {
			id, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "codex", ExternalID: fmt.Sprint("extra", i), Title: fmt.Sprint("Session title ", i), Cwd: "/fixture", LastActivityAt: now}, RepoID: &project, Label: "idle"})
			if err != nil {
				return err
			}
			if err := tx.UpsertInboxItem(model.InboxItem{ID: fmt.Sprint("extra", i), SessionID: id, Kind: "finished", TS: now.Add(-2 * time.Minute)}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := s.loadPage(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Board.Needs) != 1 || p.Board.Pulse["waiting"] != 1 {
		t.Fatal("completion events entered live Needs you", len(p.Board.Needs), p.Board.Pulse)
	}
	body := request(s, "GET", "/flow").Body.String()
	if !strings.Contains(body, "Activity · notifications") || !strings.Contains(body, "Session title 8") {
		t.Fatal("history lost")
	}
	if strings.Count(body, "data-need-key=") != 1 {
		t.Fatal("live wait duplicated")
	}

}

func TestProjectAggregatedStates(t *testing.T) {
	s, st, _, project := fixture(t)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		for i, label := range []string{"~idle", "working", "idle"} {
			if _, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "codex", ExternalID: fmt.Sprint(i), Cwd: "/fixture", LastActivityAt: time.Now()}, RepoID: &project, Label: label}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	body := request(s, "GET", "/projects").Body.String()
	for _, text := range []string{"1 working", "1 waiting", "2 idle"} {
		if !strings.Contains(body, text) {
			t.Errorf("missing aggregate %q", text)
		}
	}
	if strings.Contains(body, "0 finished") {
		t.Error("zero state displayed")
	}
}

func TestFreshnessCompactAndActivityEmpty(t *testing.T) {
	s, st, _, _ := fixture(t)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		for _, name := range []string{"cwds", "otlp"} {
			if err := tx.SetSourceOK(name, time.Now().Add(-time.Hour)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	body := request(s, "GET", "/").Body.String()
	if !strings.Contains(body, "Stale: cwds") || !strings.Contains(body, `class="seg intake-status"`) || !strings.Contains(body, "last successful reading") {
		t.Error("missing compact stale line with hover details")
	}
	p := pageData{Page: "/"}
	p.prepare()
	var out strings.Builder
	if err := s.templates.ExecuteTemplate(&out, "activity", p); err != nil {
		t.Fatal(err)
	}
	for _, note := range []string{"History builds while the collector runs", "Events need agent hooks: <code>driga hooks install</code>"} {
		if !strings.Contains(out.String(), note) {
			t.Errorf("missing %q", note)
		}
	}
}

func TestChartLoadGaps(t *testing.T) {
	data := templateFuncs()["chartData"].(func([]view.ActivityBucket) string)([]view.ActivityBucket{{Events: 2}, {Sample: &model.MachineSample{Load1: 3}}})
	if !strings.Contains(data, `[null,3]`) {
		t.Fatal(data)
	}
}

func TestAllSessionDetailLinksKeepFilter(t *testing.T) {
	s, st, _, project := fixture(t)
	var id int64
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		var err error
		ended := time.Now()
		id, err = tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "codex", ExternalID: "ended-link", Title: "Ended link fixture", LastActivityAt: ended}, EndedAt: &ended, RepoID: &project, Label: "finished"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	body := request(s, "GET", "/agents?all=1").Body.String()
	if !strings.Contains(body, fmt.Sprintf(`/agents/%d?all=1&amp;machine=all`, id)) {
		t.Error("ended session link loses all filter")
	}
	if w := request(s, "GET", fmt.Sprintf("/agents/%d?all=1", id)); w.Code != 200 {
		t.Fatalf("ended detail status: %d", w.Code)
	}
}

// The desktop shell owns the window title, view menu, settings and dialogs,
// so the page carries none of them.
func TestPageLeavesChromeToShell(t *testing.T) {
	s, _, agent, _ := fixture(t)
	title := regexp.MustCompile(`(?s)<head>.*?<title>([^<]*)</title>.*?</head>`)
	for _, path := range []string{"/", "/flow", "/projects", "/machines", fmt.Sprint("/agents/", agent), "/help/keys"} {
		w := request(s, "GET", path)
		if w.Code != 200 {
			t.Fatalf("%s: %d", path, w.Code)
		}
		body := w.Body.String()
		if m := title.FindStringSubmatch(body); m == nil || m[1] != "Driga" {
			t.Errorf("%s: title %q", path, m)
		}
		for _, banned := range []string{`<nav class="wins"`, `data-window=`, `class="keys"`, `id="theme"`, `theme-control`, `theme latte`, `data-inbox-copy`} {
			if strings.Contains(body, banned) {
				t.Errorf("%s still has %s", path, banned)
			}
		}
		if path == "/" && strings.Contains(body, "<nav") {
			t.Error("/ still has a tab bar")
		}
		if !strings.Contains(body, `data-view="flow"`) || !strings.Contains(body, `data-view="projects"`) || !strings.Contains(body, `data-view="machines"`) {
			t.Errorf("%s lost the in-page view switcher", path)
		}
	}
	keys := request(s, "GET", "/help/keys").Body.String()
	for _, want := range []string{`<dt>:</dt>`, `<dt>/</dt>`, `<dt>?</dt>`, `<dt>0–2</dt>`, `<dt>c</dt>`, `<dt>m / t / s</dt>`} {
		if !strings.Contains(keys, want) {
			t.Errorf("/help/keys missing %s", want)
		}
	}
}

func TestPageTakesThemeFromApp(t *testing.T) {
	s, _, _, _ := fixture(t)
	for theme, want := range map[string]string{"": "system", "system": "system", "light": "light", "dark": "dark", "mocha": "system"} {
		s.opts.Theme = theme
		if body := request(s, "GET", "/").Body.String(); !strings.Contains(body, `<html lang="en" data-theme="`+want+`">`) {
			t.Errorf("theme %q: want data-theme %q", theme, want)
		}
	}
}

func TestScriptsLeaveDialogsAndThemeToShell(t *testing.T) {
	scripts, err := fs.Glob(assets, "static/*.js")
	if err != nil || len(scripts) == 0 {
		t.Fatal(scripts, err)
	}
	native := regexp.MustCompile(`\bconfirm\(`)
	for _, name := range scripts {
		raw, _ := assets.ReadFile(name)
		if native.Match(raw) {
			t.Errorf("%s calls confirm()", name)
		}
		if strings.Contains(string(raw), `"theme"`) || strings.Contains(string(raw), "setTheme") {
			t.Errorf("%s still switches themes", name)
		}
	}
	js, _ := assets.ReadFile("static/app.js")
	for _, want := range []string{"window.drigaCommand", `view:`, `find:`, `reload:`, `openSession:`} {
		if !strings.Contains(string(js), want) {
			t.Errorf("app.js missing %s", want)
		}
	}
	for _, name := range []string{"static/chat.js", "static/flow-board.js"} {
		if raw, _ := assets.ReadFile(name); !strings.Contains(string(raw), `"/app/confirm"`) {
			t.Errorf("%s does not ask the app", name)
		}
	}
}
