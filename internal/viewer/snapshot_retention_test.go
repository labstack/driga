package viewer

import (
	"context"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestSnapshotHistoryPruning(t *testing.T) {
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now()
	old := now.Add(-2 * time.Hour)
	err = st.Tx(context.Background(), func(tx *store.Tx) error {
		for _, at := range []time.Time{old, now} {
			if err := tx.InsertSample(model.MachineSample{TS: at}); err != nil {
				return err
			}
			if err := tx.InsertEvent(model.Event{Adapter: "test", ExternalID: "busy", TS: at, Kind: "tool"}); err != nil {
				return err
			}
		}
		return tx.PruneSnapshot(now)
	})
	if err != nil {
		t.Fatal(err)
	}
	samples, err := st.Samples(time.Time{})
	if err != nil || len(samples) != 1 {
		t.Fatalf("samples: %v %v", samples, err)
	}
	events, err := st.LatestEvents(time.Time{})
	if err != nil || len(events) != 1 {
		t.Fatalf("events: %v %v", events, err)
	}
	counts, err := st.EventCounts(old, now)
	if err != nil || len(counts) != 1 {
		t.Fatalf("minutes: %v %v", counts, err)
	}
}
