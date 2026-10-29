package web

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGrantWebRoutes(t *testing.T) {
	s, _, _ := chatFixture(t)
	if w := request(s, "GET", "/chat/api/grant-history"); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if w := postChat(s, "/chat/api/grant-revoke", `{"id":"missing"}`); w.Code != 400 {
		t.Fatal(w.Code)
	}
	r := httptest.NewRequest("POST", "/chat/api/grant-revoke", strings.NewReader(`{"id":"x"}`))
	r.Host = "127.0.0.1:47801"
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}
