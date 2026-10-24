package view

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

type boardReadCounter struct {
	Reader
	bulk, sessions int
}

func (r *boardReadCounter) BoardEvidence(since, until time.Time) ([]model.Event, map[int64][]store.ObservationMinute, error) {
	r.bulk++
	return r.Reader.BoardEvidence(since, until)
}
func (r *boardReadCounter) Sessions(f store.SessionFilter) ([]model.Session, error) {
	r.sessions++
	return r.Reader.Sessions(f)
}
func (*boardReadCounter) Events(string, string, time.Time) ([]model.Event, error) {
	panic("per-row event query")
}
func (*boardReadCounter) ObservationMinutes(int64, time.Time) ([]store.ObservationMinute, error) {
	panic("per-row observation query")
}

func TestLoadBoardSnapshotUsesBulkEvidence(t *testing.T) {
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now().UTC()
	err = st.Tx(context.Background(), func(tx *store.Tx) error {
		for i := 0; i < 200; i++ {
			id, e := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: fmt.Sprint(i), Title: "Work", StartedAt: now.Add(-time.Hour), LastActivityAt: now}, Label: "working"})
			if e != nil {
				return e
			}
			if e = tx.Observe(id, now, 100, 20); e != nil {
				return e
			}
			if e = tx.InsertEvent(model.Event{Adapter: "claude", ExternalID: fmt.Sprint(i), TS: now, Kind: "otlp:tool_result"}); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	r := &boardReadCounter{Reader: st}
	snap := BoardSnapshot{Nodes: []FlowNode{}}
	err = loadBoardMachine(r, Filter{}, now, &snap)
	if err != nil {
		t.Fatal(err)
	}
	if r.bulk != 1 || r.sessions != 1 || len(snap.Nodes) != 200 {
		t.Fatal("not a bulk snapshot", r.bulk, r.sessions, len(snap.Nodes))
	}
	for _, n := range snap.Nodes {
		if len(n.Events) != 1 || n.Observed == nil {
			t.Fatal("lost bulk evidence")
		}
	}
}
