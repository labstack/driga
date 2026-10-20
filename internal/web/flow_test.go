package web

import (
	"context"
	"fmt"
	"github.com/labstack/driga/internal/flowtest"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestFlowSharedDefault(t *testing.T) {
	s, st, _, _ := fixture(t)
	var want int64
	if err := st.Tx(context.Background(), func(tx *store.Tx) (err error) { want, err = flowtest.SeedDefault(tx); return err }); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"", "?machine=all", "?machine=local"} {
		p, code, err := s.loadPage(httptest.NewRequest("GET", "/flow"+scope, nil))
		if err != nil || code != 200 || p.Flow.Lead == nil || p.Flow.Lead.ID != want {
			t.Errorf("scope %q: lead = %+v, code %d, %v; want %d", scope, p.Flow.Lead, code, err, want)
		}
	}
}

func TestFlowMapLayoutParts(t *testing.T) {
	s, _, id, _ := fixture(t)
	body := request(s, "GET", fmt.Sprint("/flow?id=", id), s.token).Body.String()
	for _, part := range []string{"Needs you · machine-wide", "Session map · measured activity", "Session detail · signal mixer", "data-flow-svg", "data-flow-zoom", "data-flow-quiet", "/static/flow-map.js", "/static/flow-map.css"} {
		if !strings.Contains(body, part) {
			t.Error("missing", part)
		}
	}
	if strings.Index(body, "metro-needs") > strings.Index(body, "metro-main") {
		t.Fatal("attention must come first")
	}
}

func TestFlowExpansionSurvivesRefresh(t *testing.T) {
	s, _, id, _ := fixture(t)
	p, code, err := s.loadPage(httptest.NewRequest("GET", fmt.Sprintf("/flow?id=%d&quiet=1&machine=all", id), nil))
	if err != nil || code != 200 {
		t.Fatal(code, err)
	}
	if !p.FlowExpanded || !strings.Contains(p.RefreshURL, "quiet=1") {
		t.Fatal("refresh collapses expanded threads", p.RefreshURL)
	}
	if strings.Contains(p.FlowToggle, "quiet=1") || !strings.Contains(p.FlowToggle, fmt.Sprint("id=", id)) {
		t.Fatal("collapse link loses session", p.FlowToggle)
	}
}

func TestFlowPageCSP(t *testing.T) {
	s, _, id, _ := fixture(t)
	w := request(s, "GET", fmt.Sprint("/flow?id=", id), s.token)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{"signal mixer", "Session map", "data-flow-map", "data-window=\"7\"", "&lt;script&gt;alert(1)&lt;/script&gt;"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s", want)
		}
	}
	if regexp.MustCompile(`(?i)\sstyle=|\son[a-z]+=|<script[^>]*>\s*[^<\s]`).MatchString(body) || strings.Contains(body, title) {
		t.Fatal("unescaped text or CSP violation")
	}
	if w.Header().Get("Content-Security-Policy") != csp {
		t.Fatal(w.Header())
	}
}

func TestFlowEmptyAndBadID(t *testing.T) {
	s, _, _, _ := fixture(t)
	body := request(s, "GET", "/flow", s.token).Body.String()
	if !strings.Contains(body, "No live interactive session") {
		t.Fatal(body)
	}
	for _, id := range []string{"0", "-1", "wrong"} {
		if w := request(s, "GET", "/flow?id="+id, s.token); w.Code != 400 {
			t.Fatal(id, w.Code)
		}
	}
}

func TestFlowRenderingPolish(t *testing.T) {
	s, _, _, _ := fixture(t)
	p := pageData{Flow: view.FlowView{Lead: &view.FlowNode{Title: "<script>alert(1)</script>", Adapter: "claude", Mixer: model.StateMixer{Verdict: "finished", Evidence: "stored", Rows: []model.StateSignal{{Name: "hooks", Reading: "Notification"}}}}}}
	var out strings.Builder
	if err := s.templates.ExecuteTemplate(&out, "flow", p); err != nil {
		t.Fatal(err)
	}
	body := out.String()
	if !strings.Contains(body, `title="Notification"`) || strings.Contains(body, "<script>") {
		t.Fatal("unescaped title or missing signal tooltip", body)
	}
	if strings.Contains(body, "data-wire-return") {
		t.Fatal("return line cannot prove delivery")
	}
}
