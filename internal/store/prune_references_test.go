package store

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/labstack/driga/internal/feed"
	"github.com/labstack/driga/internal/model"
)

func TestPruneKeepsSessionsReferencedByInbox(t *testing.T) {
	for _, reference := range []string{"inbox", "event", "handoff", "expired inbox", "none"} {
		t.Run(reference, func(t *testing.T) {
			st := openTest(t, "", Memory)
			now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
			old := now.Add(-31 * 24 * time.Hour)
			var id int64
			mustTx(t, st, func(tx *Tx) (err error) {
				id, err = tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "codex", ExternalID: "old"}, EndedAt: &old})
				if err != nil {
					return err
				}
				switch reference {
				case "inbox":
					return tx.UpsertInboxItem(model.InboxItem{ID: "recent", SessionID: id, TS: now})
				case "expired inbox":
					return tx.UpsertInboxItem(model.InboxItem{ID: "old", SessionID: id, TS: old})
				case "event":
					return tx.InsertEvent(model.Event{Adapter: "codex", ExternalID: "old", TS: now, Kind: "Stop"})
				case "handoff":
					return tx.InsertHandoff(id, now, "answer")
				}
				return nil
			})
			mustTx(t, st, func(tx *Tx) error { return tx.Prune(now) })
			_, err := st.Session(id)
			keep := reference != "none" && reference != "expired inbox"
			if keep && err != nil {
				t.Fatalf("retained %s lost its session: %v", reference, err)
			}
			if !keep && !errors.Is(err, ErrNotFound) {
				t.Fatalf("unreferenced session not pruned: %v", err)
			}
			epoch, min, _, _ := st.FeedBounds()
			rows, err := st.FeedSince(epoch, min-1, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				var deletion feed.DeleteRow
				if row.Kind != string(feed.Delete) {
					continue
				}
				if err := json.Unmarshal(row.Payload, &deletion); err != nil {
					t.Fatal(err)
				}
				if keep && deletion.Kind == feed.Session && deletion.Key == feed.Key("codex", "old") {
					t.Fatal("retained session emitted a delete")
				}
			}
		})
	}
}
