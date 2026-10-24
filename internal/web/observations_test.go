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

func TestFlowObservedRendering(t *testing.T) {
	s, st, id, _ := fixture(t)
	at := time.Now().Add(-time.Second).UTC().Truncate(time.Millisecond)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		child, err := tx.UpsertSession(model.Session{ParentID: &id, SessionObs: model.SessionObs{Adapter: "codex", ExternalID: "observed-child"}, Label: "working"})
		if err != nil {
			return err
		}
		if err = tx.Observe(child, at, 123, 0); err != nil {
			return err
		}
		return tx.Observe(id, at, 12288, 250)
	}); err != nil {
		t.Fatal(err)
	}
	body := request(s, "GET", fmt.Sprint("/flow.json?id=", id), s.token).Body.String()
	for _, want := range []string{`"last_growth_at":"` + at.Format(time.RFC3339Nano) + `"`, `"last_growth_bytes":12288`, `"last_growth_bytes":123`, `"from_at"`, `"cpu_ms_per_min"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s", want)
		}
	}

}
