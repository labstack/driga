package viewer

import (
	"context"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

func TestScopedOverviewDoesNotBuildDetails(t *testing.T) {
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	err = st.Tx(context.Background(), func(tx *store.Tx) error {
		_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: "one", LastActivityAt: time.Now()}, Label: "waiting"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	s := NewStoreSource(st, t.TempDir(), view.Filter{})
	snap, err := s.LoadScope(context.Background(), store.Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.AgentDetails) != 0 || len(snap.ProjectDetails) != 0 {
		t.Fatalf("overview built details: %d/%d", len(snap.AgentDetails), len(snap.ProjectDetails))
	}
}

func TestOverviewDefersSessionDetailsToSelection(t *testing.T) {
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var id int64
	err = st.Tx(context.Background(), func(tx *store.Tx) error {
		var err error
		id, err = tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: "selected", LastActivityAt: time.Now()}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	src := NewStoreSource(st, t.TempDir(), view.Filter{})
	snap, err := src.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.AgentDetails) != 0 {
		t.Fatal("overview eagerly loaded sessions")
	}
	detail, err := src.LoadAgent(context.Background(), store.Scope{}, id)
	if err != nil || detail.Session.ID != id {
		t.Fatal(detail, err)
	}
}
