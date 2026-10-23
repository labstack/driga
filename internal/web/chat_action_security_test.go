package web

import (
	"context"
	"fmt"
	"testing"

	"github.com/labstack/driga/internal/chat"
)

func TestChatActionPOSTSecurity(t *testing.T) {
	s, b, id := chatFixture(t)
	for _, route := range []string{"mode", "decide"} {
		body := fmt.Sprintf(`{"id":%q,"mode":"full auto","action":"missing","allow":true}`, id)
		for _, tc := range []struct {
			origin, csrf, token string
			code                int
		}{
			{"http://127.0.0.1:4321", s.csrf, "", 401},
			{"https://evil.test", s.csrf, s.token, 403},
			{"", s.csrf, s.token, 403},
			{"http://127.0.0.1:4321", "wrong", s.token, 403},
		} {
			if w := postChat(s, "/chat/api/"+route, body, tc.origin, tc.csrf, tc.token); w.Code != tc.code {
				t.Fatalf("%s: %d %s", route, w.Code, w.Body)
			}
		}
		if w := request(s, "GET", "/chat/api/"+route, s.token); w.Code != 405 {
			t.Fatal(w.Code)
		}
	}
	if w := postChat(s, "/chat/api/mode", fmt.Sprintf(`{"id":%q,"mode":"full auto"}`, id), "http://127.0.0.1:4321", s.csrf, s.token); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	state, err := b.Read(context.Background(), id, 0)
	if err != nil || state.Mode != chat.FullAuto {
		t.Fatal(state, err)
	}
}
