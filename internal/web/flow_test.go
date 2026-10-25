package web

import (
	"context"
	"fmt"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/flowtest"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

func TestFlowSharedDefault(t *testing.T) {
	s, st, _, _ := fixture(t)
	var want int64
	if err := st.Tx(context.Background(), func(tx *store.Tx) (err error) { want, err = flowtest.SeedDefault(tx); return err }); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"?view=family", "?view=family&machine=all", "?view=family&machine=local"} {
		p, code, err := s.loadPage(httptest.NewRequest("GET", "/flow"+scope, nil))
		if err != nil || code != 200 || p.Flow.Lead == nil || p.Flow.Lead.ID != want {
			t.Errorf("scope %q: lead = %+v, code %d, %v; want %d", scope, p.Flow.Lead, code, err, want)
		}
	}
}

func TestFlowMapLayoutParts(t *testing.T) {
	s, _, id, _ := fixture(t)
	body := request(s, "GET", fmt.Sprint("/flow?id=", id)).Body.String()
	for _, part := range []string{"Needs you", "Selected family", "data-flow-surface=\"map\"", "data-flow-surface=\"timeline\"", "fb-legend", "/static/flow-board.js", "/static/flow-board.css"} {
		if !strings.Contains(body, part) {
			t.Error("missing", part)
		}
	}
	if strings.Index(body, "metro-needs") > strings.Index(body, "metro-main") {
		t.Fatal("attention must come first")
	}
}

func TestFlowViewAndSelectionSurviveRefreshURL(t *testing.T) {
	s, _, id, _ := fixture(t)
	p, code, err := s.loadPage(httptest.NewRequest("GET", fmt.Sprintf("/flow?id=%d&view=timeline&machine=all", id), nil))
	if err != nil || code != 200 {
		t.Fatal(code, err)
	}
	if !strings.Contains(p.RefreshURL, "view=timeline") || !strings.Contains(p.RefreshURL, fmt.Sprint("id=", id)) {
		t.Fatal(p.RefreshURL)
	}
}

func TestFlowPageCSP(t *testing.T) {
	s, _, id, _ := fixture(t)
	w := request(s, "GET", fmt.Sprint("/flow?id=", id))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{"Selected family", "fb-map", "data-flow-board", "data-window=\"0\"", "&lt;script&gt;alert(1)&lt;/script&gt;"} {
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
	body := request(s, "GET", "/flow").Body.String()
	if !strings.Contains(body, "data-flow-board") {
		t.Fatal(body)
	}
	for _, id := range []string{"0", "-1", "wrong"} {
		if w := request(s, "GET", "/flow?id="+id); w.Code != 400 {
			t.Fatal(id, w.Code)
		}
	}
}

func TestFlowRenderingPolish(t *testing.T) {
	s, _, _, _ := fixture(t)
	p := pageData{Board: view.Board(view.BoardSnapshot{Nodes: []view.FlowNode{{ID: 1, Title: "<script>alert(1)</script>", Adapter: "claude", Label: "finished", Mixer: model.StateMixer{Evidence: "stored"}}}}, time.Hour, time.Now())}
	var out strings.Builder
	if err := s.templates.ExecuteTemplate(&out, "flow", p); err != nil {
		t.Fatal(err)
	}
	body := out.String()
	if !strings.Contains(body, `&lt;script&gt;alert(1)&lt;/script&gt;`) || strings.Contains(body, "<script>") {
		t.Fatal("unescaped title or missing signal tooltip", body)
	}
	if strings.Contains(body, "data-wire-return") {
		t.Fatal("return line cannot prove delivery")
	}
}
