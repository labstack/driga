package web

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/view"
)

func TestThemeControlAndStylesheet(t *testing.T) {
	s, _, _, _ := fixture(t)
	body := request(s, "GET", "/", s.token).Body.String()
	for _, want := range []string{`/static/theme.css`, `id="theme"`, `value="system"`, `value="mocha"`, `value="latte"`, `system`, `mocha`, `latte`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s", want)
		}
	}
	css := request(s, "GET", "/static/theme.css", s.token)
	if css.Code != 200 || css.Body.String() != view.ThemeCSS() {
		t.Fatal("theme endpoint differs from shared source")
	}
	if strings.Contains(body, `style=`) || strings.Contains(body, `<script>`) {
		t.Fatal("inline CSS or script violates CSP")
	}
}

func TestRailCountsRefresh(t *testing.T) {
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
		for _, want := range []string{`nav-count-agents`, `nav-count-inbox`, `nav-count-cleanup`} {
			if !strings.Contains(body.String(), want) {
				t.Errorf("fragment=%v missing %s", fragment, want)
			}
		}
	}
}

func TestRailCountsOnEveryPage(t *testing.T) {
	s, _, _, _ := fixture(t)
	for _, path := range []string{"/", "/agents", "/projects", "/inbox", "/orphans", "/machines", "/cleanup", "/agents/1", "/projects/1"} {
		body := request(s, "GET", path, s.token).Body.String()
		for _, want := range []string{`id="nav-count-agents" class="count ">1</span>`, `id="nav-count-inbox" class="count hot">1</span>`, `id="nav-count-cleanup" class="count ">1</span>`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s missing count %s", path, want)
			}
		}
	}
}
