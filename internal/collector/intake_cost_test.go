package collector

import (
	"context"
	"fmt"
	"testing"

	"github.com/labstack/driga/internal/intake"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestIntakeWithoutFeedIgnoresUnrelatedSessions(t *testing.T) {
	d, st, _, _ := setup(t)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error { return tx.ConfigureFeed(false) }); err != nil {
		t.Fatal(err)
	}
	b := intake.Batch{Facts: []intake.Fact{{SessionID: "not-discovered", Name: "api_request", TS: d.now()}}}
	measure := func() float64 {
		return testing.AllocsPerRun(3, func() {
			if unmatched, err := applyIntake(context.Background(), d, st, b); err != nil || unmatched != 1 {
				t.Fatalf("unmatched=%d, error=%v", unmatched, err)
			}
		})
	}
	// Initialize source/counter rows before measuring the steady state.
	measure()
	before := measure()
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		for i := range 100 {
			if _, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: fmt.Sprint(i)}}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	after := measure()
	t.Logf("intake allocations without feed: before=%.0f; with 100 unrelated sessions=%.0f", before, after)
	if after > before+100 {
		t.Fatalf("unrelated sessions added %.0f intake allocations without feed consumers", after-before)
	}
}
