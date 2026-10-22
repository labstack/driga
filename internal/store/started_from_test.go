package store

import (
	"context"
	"testing"

	"github.com/labstack/driga/internal/model"
)

func TestStartedFromImmutableUpsert(t *testing.T) {
	st := openTest(t, "", Memory)
	var id int64
	mustTx(t, st, func(tx *Tx) error {
		var err error
		id, err = tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "session", Title: "Task"}})
		return err
	})
	s, err := st.Session(id)
	if err != nil || s.StartedFrom != "" {
		t.Fatal(s, err)
	}
	var isNull bool
	if err := st.conn.QueryRowContext(context.Background(), "SELECT started_from IS NULL FROM sessions WHERE id=?", id).Scan(&isNull); err != nil || !isNull {
		t.Fatalf("nullable started_from: %t %v", isNull, err)
	}
	for _, surface := range []string{"Fixture Terminal", "", "Different Editor"} {
		s.StartedFrom = surface
		mustTx(t, st, func(tx *Tx) error { _, err := tx.UpsertSession(s); return err })
		got, err := st.Session(s.ID)
		if err != nil || got.StartedFrom != "Fixture Terminal" {
			t.Fatalf("upsert %q: %+v %v", surface, got, err)
		}
	}
	// Remote sessions use the same invariant.
	mustTx(t, st, func(tx *Tx) error {
		peer := tx.ForMachine("peer")
		for _, surface := range []string{"Fixture Editor", "", "Changed"} {
			s.StartedFrom = surface
			if _, err := peer.UpsertSession(s); err != nil {
				return err
			}
		}
		return nil
	})
	ss, err := st.SessionsScoped(SessionFilter{}, Scope{Machines: []string{"peer"}})
	if err != nil || len(ss) != 1 || ss[0].StartedFrom != "Fixture Editor" {
		t.Fatalf("peer: %+v %v", ss, err)
	}
}
