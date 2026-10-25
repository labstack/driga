package web

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestAttentionOverflowDestination(t *testing.T) {
	s, st, _, _ := fixture(t)
	err := st.Tx(context.Background(), func(tx *store.Tx) error {
		for i := 0; i < 10; i++ {
			_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: fmt.Sprint(i), Title: fmt.Sprint("Needs review ", i), LastActivityAt: time.Now()}, Label: "waiting"})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	w := request(s, "GET", "/")
	if !strings.Contains(w.Body.String(), "data-show-needs") {
		t.Fatal("overflow does not link to attention")
	}
	p, _, err := s.loadPage(httptest.NewRequest("GET", "/flow?view=sessions", nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Board.Needs) != 11 {
		t.Fatalf("destination has %d needs; want 11", len(p.Board.Needs))
	}
}

// pipeResponseWriter writes to a peer that deliberately never reads. The
// controller must set a deadline before the first write to release the handler.
