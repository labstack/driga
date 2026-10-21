package tui

import (
	"context"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

// A collector whose process and machine sources failed still has sessions;
// the TUI must show them instead of failing the whole refresh.
func TestLoadWithoutMachineSample(t *testing.T) {
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "s", Cwd: "/w", Title: "visible", LastActivityAt: time.Now()}, Label: "idle"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	snap, err := NewStoreSource(st, t.TempDir(), view.Filter{}).Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(snap.Agents.Rows) != 1 {
		t.Fatalf("agents: %+v", snap.Agents.Rows)
	}
}
