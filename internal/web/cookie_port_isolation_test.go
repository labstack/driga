package web

import (
	"net/http/httptest"
	"testing"
)

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
