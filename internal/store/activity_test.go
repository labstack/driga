package store

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

// Activity counts events by machine and time on every refresh; without an
// index starting with (machine_id, ts) that reads a month of events.
func TestEventCountsUseTimeIndex(t *testing.T) {
	st, err := Open("", Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rows, err := st.db.Query(`EXPLAIN QUERY PLAN SELECT COUNT(*) FROM events WHERE machine_id=? AND ts>=? AND ts<=?`, "m", 0, time.Now().UnixNano())
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(detail + "\n")
	}
	if !strings.Contains(plan.String(), "events_machine_ts") {
		t.Fatalf("plan: %s", plan.String())
	}
}

func TestEventCountsSurviveSessionRetention(t *testing.T) {
	for _, remote := range []bool{false, true} {
		t.Run(fmt.Sprint(remote), func(t *testing.T) {
			st, err := Open("", Memory)
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			base := time.Now().Truncate(time.Minute).Add(-5 * time.Minute)
			err = st.Tx(context.Background(), func(tx *Tx) error {
				for i := 0; i < 250; i++ {
					e := model.Event{Adapter: "claude", ExternalID: "busy", Kind: "tool", TS: base.Add(time.Duration(i) * time.Second)}
					if remote {
						err = tx.ForMachine("peer").ApplyEvent(e)
					} else {
						err = tx.InsertEvent(e)
					}
					if err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			q := st.ReadMachine(st.MachineID())
			if remote {
				q = st.ReadMachine("peer")
			}
			counts, err := q.EventCounts(base, base.Add(5*time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			total := 0
			for _, c := range counts {
				total += c.Count
			}
			if total != 250 || len(counts) != 5 {
				t.Fatalf("counts: %+v total %d", counts, total)
			}
			if remote {
				err = st.Tx(context.Background(), func(tx *Tx) error {
					return tx.ForMachine("peer").ApplyEvent(model.Event{Adapter: "claude", ExternalID: "busy", Kind: "tool", TS: base.Add(249 * time.Second)})
				})
				if err != nil {
					t.Fatal(err)
				}
				counts, _ = q.EventCounts(base, base.Add(5*time.Minute))
				total = 0
				for _, c := range counts {
					total += c.Count
				}
				if total != 250 {
					t.Fatalf("duplicate counted: %d", total)
				}
			}
		})
	}
}

func TestEventMinutesBackfillAndPrune(t *testing.T) {
	st, err := Open("", Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now().Truncate(time.Minute)
	old := now.Add(-31 * 24 * time.Hour)
	if err = st.Tx(context.Background(), func(tx *Tx) error {
		for _, at := range []time.Time{old, now} {
			if err := tx.InsertEvent(model.Event{Adapter: "test", ExternalID: "seed", TS: at, Kind: "tool"}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Recreate the pre-0007 schema and execute its actual migration against rows.
	if _, err = st.db.Exec("DROP TABLE event_minutes"); err != nil {
		t.Fatal(err)
	}
	migration, err := migrations.ReadFile("migrations/0007_event_minutes.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := strings.Split(string(migration), "-- +goose Down")[0]
	if _, err = st.db.Exec(up); err != nil {
		t.Fatal(err)
	}
	counts, err := st.EventCounts(old, now)
	if err != nil || len(counts) != 2 {
		t.Fatalf("backfill %v %v", counts, err)
	}
	if err = st.Tx(context.Background(), func(tx *Tx) error { return tx.Prune(now) }); err != nil {
		t.Fatal(err)
	}
	counts, err = st.EventCounts(old, now)
	if err != nil || len(counts) != 1 || counts[0].Count != 1 {
		t.Fatalf("prune %v %v", counts, err)
	}
}
