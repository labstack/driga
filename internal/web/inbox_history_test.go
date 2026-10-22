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

func TestInboxHistoryPageBoundsHTML(t *testing.T) {
	s, st, agent, _ := fixture(t)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		for n := range 250 {
			if err := tx.UpsertInboxItem(model.InboxItem{ID: fmt.Sprintf("stop-%03d", n), SessionID: agent, Kind: "finished", TS: time.Now().Add(-time.Duration(n) * time.Minute)}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/inbox", "/inbox?all=1", "/inbox?all=1&page=2"} {
		w := request(s, "GET", path, s.token)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
		body := w.Body.String()
		if n := strings.Count(body, "data-inbox-key="); n > 100 {
			t.Errorf("%s emitted %d rows", path, n)
		}
		if !strings.Contains(body, "history") {
			t.Errorf("%s lacks history navigation", path)
		}
	}
}
