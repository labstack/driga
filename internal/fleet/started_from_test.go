package fleet

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/labstack/driga/internal/feed"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestStartedFromFleetAdditiveDecode(t *testing.T) {
	for _, surface := range []string{"", "Fixture Editor"} {
		st := remoteStore(t)
		at := time.Now().UTC()
		snap := remoteSnapshot(t, at, "epoch")
		_, payload := feed.Encode(feed.Session, model.Session{StartedFrom: surface, SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "remote", Hosted: true, LastActivityAt: at}}, feed.None)
		snap.Data.Sessions = []json.RawMessage{payload}
		apply(t, st, Batch{MachineID: "remote", Snapshot: snap}, at)
		ss, err := st.SessionsScoped(store.SessionFilter{IncludeEnded: true}, store.Scope{})
		if err != nil || len(ss) != 1 || ss[0].StartedFrom != surface {
			t.Fatalf("surface %q: %+v, %v", surface, ss, err)
		}
	}
}
