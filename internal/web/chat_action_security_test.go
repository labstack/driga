package web

import (
	"context"
	"fmt"
	"testing"

	"github.com/labstack/driga/internal/chat"
)

func TestChatActionPOSTs(t *testing.T) {
	s, b, id := chatFixture(t)
	for _, route := range []string{"mode", "decide"} {
		if w := request(s, "GET", "/chat/api/"+route); w.Code != 405 {
			t.Fatal(w.Code)
		}
	}
	if w := postChat(s, "/chat/api/mode", fmt.Sprintf(`{"id":%q,"mode":"full auto"}`, id)); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	state, err := b.Read(context.Background(), id, 0)
	if err != nil || state.Mode != chat.FullAuto {
		t.Fatal(state, err)
	}
}
