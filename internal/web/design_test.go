package web

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/view"
)

func TestThemeStylesheet(t *testing.T) {
	s, _, _, _ := fixture(t)
	body := request(s, "GET", "/").Body.String()
	for _, want := range []string{`/static/theme.css`, `data-theme="system"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s", want)
		}
	}
	css := request(s, "GET", "/static/theme.css")
	if css.Code != 200 || css.Body.String() != view.ThemeCSS() {
		t.Fatal("theme endpoint differs from shared source")
	}
	if strings.Contains(body, `style=`) || strings.Contains(body, `<script>`) {
		t.Fatal("inline CSS or script violates CSP")
	}
}

func TestFooterDescribesChatActions(t *testing.T) {
	s, _, _, _ := fixture(t)
	for _, page := range []string{"/", "/chat", "/flow"} {
		body := request(s, "GET", page).Body.String()
		if strings.Contains(body, "Read-only dashboard") || !strings.Contains(body, "Local actions in Chat") {
			t.Fatal(page, "footer misrepresents available actions")
		}
	}
}

func TestViewSwitcherRefresh(t *testing.T) {
	s, _, _, _ := fixture(t)
	for _, fragment := range []bool{false, true} {
		p, _, err := s.loadPage(httptest.NewRequest("GET", "/", nil))
		if err != nil {
			t.Fatal(err)
		}
		var body strings.Builder
		name := "page"
		if fragment {
			name = "content"
		}
		if err := s.templates.ExecuteTemplate(&body, name, p); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`data-view="flow"`, `data-view="projects"`, `data-view="machines"`} {
			if !strings.Contains(body.String(), want) {
				t.Errorf("fragment=%v missing %s", fragment, want)
			}
		}
	}
}

func TestViewSwitcherOnEveryPage(t *testing.T) {
	s, _, _, _ := fixture(t)
	for _, path := range []string{"/", "/agents", "/projects", "/inbox", "/orphans", "/machines", "/cleanup", "/agents/1", "/projects/1"} {
		body := request(s, "GET", path).Body.String()
		for _, want := range []string{`data-view="flow"`, `data-view="projects"`, `data-view="machines"`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s missing %s", path, want)
			}
		}
	}
}
