package collector

import (
	"context"
	"fmt"
	"testing"

	"github.com/labstack/driga/internal/feed"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestUnchangedPassFeedAllocationBudget(t *testing.T) {
	d, st, _, _ := setup(t)
	const count = 100
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		for i := range count {
			if _, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: fmt.Sprint(i), Title: "unchanged"}}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	measure := func(snapshot bool) float64 {
		return testing.AllocsPerRun(3, func() {
			if err := st.Tx(context.Background(), func(tx *store.Tx) error {
				if !snapshot {
					for range 2 {
						if _, err := tx.Sessions(store.SessionFilter{IncludeEnded: true}); err != nil {
							return err
						}
					}
					return nil
				}
				before, err := passFeedRows(tx, d.now(), feed.Titles)
				if err != nil {
					return err
				}
				after, err := passFeedRows(tx, d.now(), feed.Titles)
				if err != nil {
					return err
				}
				return appendPassChanges(tx, before, after, d.now())
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
	reads, snapshots := measure(false), measure(true)
	t.Logf("100 unchanged sessions: reads=%.0f allocations; feed snapshots/diff=%.0f", reads, snapshots)
	// SQL scanning is still linear. Comparing unchanged rows must add only
	// cheap keys/typed snapshots, rather than JSON objects for every field.
	if snapshots-reads > 100*count+1000 {
		t.Fatalf("unchanged feed comparison adds %.0f allocations for %d rows", snapshots-reads, count)
	}
	if rows := feedRows(t, st, 0); len(rows) != 0 {
		t.Fatalf("unchanged snapshots published %d rows", len(rows))
	}
}
