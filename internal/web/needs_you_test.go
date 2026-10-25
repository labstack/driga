package web

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestNeedsYouInboxJumps(t *testing.T) {
	s, st, id, _ := fixture(t)
	at := time.Now().UTC()
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		return tx.UpsertInboxItem(model.InboxItem{ID: "needs-permission", SessionID: id, Kind: "waiting", Reason: "permission", Tool: "Bash", TS: at, WaitingSinceAt: at, Jump: &model.InboxJump{Kind: "resume", Command: "claude --resume 'verified'", Cwd: "/repo"}})
	}); err != nil {
		t.Fatal(err)
	}
	w := request(s, "GET", "/inbox")
	body := w.Body.String()
	for _, want := range []string{"permission", "Bash", "data-inbox-jump", fmt.Sprintf(`data-session="%d"`, id), "/repo"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Contains(body, "claude --resume") {
		t.Error("Jump replaced by a command to copy")
	}
	if request(s, "POST", "/inbox").Code < 400 {
		t.Fatal("inbox accepted POST")
	}
}
