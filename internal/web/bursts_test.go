package web

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPromptBurstWebRoutes(t *testing.T) {
	s, _, _ := chatFixture(t)
	if w := request(s, "GET", "/chat/api/prompt-bursts"); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if w := postChat(s, "/chat/api/burst-decide", `{"id":"missing","members":["one"],"allow":true}`); w.Code != 400 {
		t.Fatal(w.Code, w.Body)
	}
	if w := postChat(s, "/chat/api/burst-decide", `{"id":"missing","members":["one"],"allow":true,"similar":true}`); w.Code != 400 {
		t.Fatal("smuggled similar", w.Code)
	}
	r := httptest.NewRequest("POST", "/chat/api/burst-decide", strings.NewReader(`{}`))
	r.Host = "127.0.0.1:47801"
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}
