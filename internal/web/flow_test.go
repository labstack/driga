package web

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

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
