package view

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestNeedsYouSharedOrderingAndDuration(t *testing.T) {
	st := fixture(t, func(tx *store.Tx) error {
		for _, id := range []string{"old", "new"} {
			if _, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: id}, Label: "waiting"}); err != nil {
				return err
			}
		}
		for _, i := range []model.InboxItem{{ID: "done", Kind: "finished", TS: testNow}, {ID: "new", SessionID: 2, Kind: "waiting", Reason: "question", WaitingSinceAt: testNow.Add(-time.Minute), TS: testNow.Add(-time.Minute)}, {ID: "old", SessionID: 1, Kind: "waiting", Reason: "permission", WaitingSinceAt: testNow.Add(-time.Hour), TS: testNow.Add(-time.Hour)}} {
			if err := tx.UpsertInboxItem(i); err != nil {
				return err
			}
		}
		return nil
	})
	v, err := Inbox(st, Filter{}, testNow)
	if err != nil || len(v.Rows) != 3 || v.Rows[0].ID != "old" || v.Rows[1].ID != "new" {
		t.Fatalf("shared order: %+v %v", v, err)
	}
	b, _ := json.Marshal(v.Rows[0])
	var row map[string]any
	json.Unmarshal(b, &row)
	if row["waited_ms"] != float64(time.Hour.Milliseconds()) {
		t.Fatalf("missing render duration: %s", b)
	}
}

func TestNeedsYouFinishedOmitsWaitStart(t *testing.T) {
	b, _ := json.Marshal(model.InboxItem{Kind: "finished", TS: testNow})
	var row map[string]any
	json.Unmarshal(b, &row)
	if _, ok := row["waiting_since_at"]; ok {
		t.Fatalf("finished has wait start: %s", b)
	}
}
