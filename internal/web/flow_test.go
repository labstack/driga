package web

import (
	"fmt"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestFlowTailWholeRowsAndSharedPane(t *testing.T) {
	s, _, id, _ := fixture(t)
	body := request(s, "GET", fmt.Sprint("/flow?id=", id), s.token).Body.String()
	if !regexp.MustCompile(`<section class="[^"]*tail-pane[^"]*flow-tail`).MatchString(body) {
		t.Error("flow must reuse the shared tail pane spacing")
	}
	css := request(s, "GET", "/static/app.css", s.token).Body.String()
	property := func(selector, name string) int {
		t.Helper()
		rule := regexp.MustCompile(`(?s)` + regexp.QuoteMeta(selector) + `\s*\{([^}]+)\}`).FindString(css)
		match := regexp.MustCompile(name + `:\s*(\d+)px`).FindStringSubmatch(rule)
		if len(match) != 2 {
			t.Fatalf("missing %s in %s", name, rule)
		}
		n, _ := strconv.Atoi(match[1])
		return n
	}
	height, row := property(".flow-tail .tail", "height"), property(".tail div", "line-height")
	if height%row != 0 {
		t.Errorf("flow tail clips oldest row at scroll bottom: %dpx / %dpx", height, row)
	}
	for selector, want := range map[string]string{".tail": "justify-content:flex-start", ".tail div": "flex-shrink:0", ".tail>div:first-child": "margin-top:auto"} {
		rule := regexp.MustCompile(`(?s)` + regexp.QuoteMeta(selector) + `\s*\{([^}]+)\}`).FindString(css)
		if !strings.Contains(rule, want) {
			t.Errorf("shared tail rule missing %s: %s", want, rule)
		}
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
	for _, want := range []string{"signal mixer", "this session family", "data-flow", "data-window=\"7\"", "&lt;script&gt;alert(1)&lt;/script&gt;"} {
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
	p := pageData{Flow: view.FlowView{Lead: &view.FlowNode{Adapter: "claude", Label: "finished"}, Mixer: model.StateMixer{Verdict: "finished", Evidence: "because hooks: unacknowledged Stop", Rows: []model.StateSignal{{Name: "hooks", Reading: "Notification"}}}}}
	for i := 0; i < 11; i++ {
		p.Flow.Children = append(p.Flow.Children, view.FlowNode{ID: int64(i + 1), Adapter: "codex", ExternalID: fmt.Sprint(i), Label: "idle", LastActivityAt: time.Now()})
	}
	var out strings.Builder
	if err := s.templates.ExecuteTemplate(&out, "flow", p); err != nil {
		t.Fatal(err)
	}
	body := out.String()
	if strings.Contains(body, "Session ") || !strings.Contains(body, "codex thread") {
		t.Error("bare ID/empty title fallback")
	}
	if strings.Count(body, "data-flow-member") != 11 {
		t.Error("collapsed threads lost live family membership")
	}
	if strings.Count(body, "data-worker data-flow-node") != 5 || !strings.Contains(body, "+6 quiet threads") {
		t.Error("workers not capped")
	}
	if !strings.Contains(body, `class="flow-value" title="Notification"`) {
		t.Error("mixer value missing tooltip")
	}
	if strings.Contains(body, " ·  · ") {
		t.Error("empty worker detail adds doubled separator")
	}
	if !strings.Contains(body, "data-wire-trunk") || strings.Contains(body, "data-wire-return") {
		t.Error("missing shared trunk")
	}
	p.FlowExpanded = true
	out.Reset()
	if err := s.templates.ExecuteTemplate(&out, "flow", p); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), "data-worker data-flow-node") != 11 || strings.Contains(out.String(), "+6 quiet threads") {
		t.Error("expanded workers remain capped")
	}
	p.FlowExpanded = false
	p.ObservedAt = time.Now()
	for i := range p.Flow.Children {
		p.Flow.Children[i].Kind = "hosted"
		p.Flow.Children[i].LastActivityAt = p.ObservedAt.Add(-2 * time.Hour)
	}
	out.Reset()
	if err := s.templates.ExecuteTemplate(&out, "flow", p); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `flow-quiet sel`) {
		t.Error("only quiet threads: Enter has no selected summary")
	}
	css := request(s, "GET", "/static/app.css", s.token).Body.String()
	rule := regexp.MustCompile(`(?s)\.flow-value\s*\{([^}]+)\}`).FindString(css)
	for _, want := range []string{"white-space:nowrap", "text-overflow:ellipsis", "min-width:0"} {
		if !strings.Contains(rule, want) {
			t.Error("value wrapping:", want)
		}
	}
}
