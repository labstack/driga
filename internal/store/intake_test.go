package store

import (
	"context"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestIntakeStateRoundTrip(t *testing.T) {
	st := openTest(t, "", Memory)
	var id int64
	mustTx(t, st, func(tx *Tx) error {
		var err error
		id, err = tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "existing"}, Label: "idle"})
		return err
	})
	s, err := st.Session(id)
	if err != nil {
		t.Fatal(err)
	}
	state, err := st.IntakeState(id)
	if err != nil || !state.LastDecision.IsZero() || !state.LastAPIRequest.IsZero() || state.LastAPIError != "" {
		t.Fatal(state, err)
	}
	want := IntakeState{LastDecision: time.Unix(1792324800, 0).UTC(), LastAPIRequest: time.Unix(1792324801, 0).UTC(), LastAPIError: "API error 529, attempt 3"}
	if err := st.Tx(context.Background(), func(tx *Tx) error {
		if err := tx.SetIntakeState(id, want); err != nil {
			return err
		}
		_, err := tx.UpsertSession(s)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	got, err := st.IntakeState(id)
	if err != nil || got != want {
		t.Fatal(got, err)
	}
}
