package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestFlowMigrationAndMetadataRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flow.db")
	st, err := Open(path, ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	tokens := int64(120)
	cost := 0.12
	var id int64
	mixed := model.StateMixer{Verdict: "working", ObservedAt: now, Rows: []model.StateSignal{{Name: "hooks", At: now, Age: 3 * time.Second, Strength: .5, Supports: true}}}
	err = st.Tx(context.Background(), func(tx *Tx) error {
		var err error
		id, err = tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "one"}, Label: "working", Mixer: &mixed})
		if err != nil {
			return err
		}
		return tx.InsertEvent(model.Event{Adapter: "claude", ExternalID: "one", TS: now, Kind: "otlp:api_request", Metadata: &model.EventMetadata{Model: "claude-test", InputTokens: &tokens, CostUSD: &cost}})
	})
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	st, err = Open(path, ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s, err := st.Session(id)
	if err != nil || s.Mixer == nil || s.Mixer.Rows[0].Age != 3*time.Second || !s.Mixer.ObservedAt.Equal(now) {
		t.Fatal(s, err)
	}
	ev, err := st.Events("claude", "one", time.Time{})
	if err != nil || len(ev) != 1 || ev[0].Metadata == nil || *ev[0].Metadata.InputTokens != 120 || *ev[0].Metadata.CostUSD != cost {
		t.Fatal(ev, err)
	}
}
