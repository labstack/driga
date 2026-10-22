package fleet

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/labstack/driga/internal/feed"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestEndedSurvivesSnapshotAndFeed(t *testing.T) {
	st := remoteStore(t)
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	snap := remoteSnapshot(t, now, "epoch")
	s := model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "one", LastActivityAt: now}, Label: "ended", EndedAt: &now}
	snap.Data.Sessions = []json.RawMessage{raw(t, feed.SessionRow{Session: s})}
	apply(t, st, Batch{MachineID: "remote", Snapshot: snap}, now)
	for i := 0; i < 2; i++ {
		ss, err := st.SessionsScoped(store.SessionFilter{IncludeEnded: true}, store.Scope{})
		if err != nil || len(ss) != 1 || ss[0].Label != "ended" || ss[0].EndedAt == nil {
			t.Fatalf("ended label lost: %+v %v", ss, err)
		}
		if i == 0 {
			key, payload := feed.Encode(feed.Session, feed.SessionRow{Session: s}, feed.None)
			if payload == nil {
				t.Fatal("missing feed payload")
			}
			row := FeedRow{Seq: 1, Kind: string(feed.Session), Key: key, Payload: payload}
			apply(t, st, Batch{MachineID: "remote", Rows: []FeedRow{row}, Cursor: Cursor{"epoch", 1}}, now)
		}
	}
}
