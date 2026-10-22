package web

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestEndedStateFilterAndCounts(t *testing.T) {
	s, st, _, _ := fixture(t)
	now := time.Now()
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "normal-end", Title: "Normal end", LastActivityAt: now}, Label: "ended", EndedAt: &now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path  string
		count int
	}{{"/agents?state=ended", 0}, {"/agents?all=1&state=ended", 1}} {
		p, code, err := s.loadPage(httptest.NewRequest("GET", tc.path, nil))
		if err != nil || code != 200 || len(p.AgentRows) != tc.count {
			t.Fatalf("%s: %+v %d %v", tc.path, p.AgentRows, code, err)
		}
		if tc.count == 1 {
			found := false
			for _, state := range p.States {
				found = found || state.Label == "ended" && state.Count == 1
			}
			if !found {
				t.Fatalf("ended missing from web totals: %+v", p.States)
			}
			body := request(s, "GET", tc.path, s.token).Body.String()
			if !strings.Contains(body, "ended") || !strings.Contains(body, "Normal end") {
				t.Fatal("ended row missing")
			}
		}
	}
}
