package collector

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/labstack/driga/internal/fleet"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestRemovePeerPurgesThroughCollector(t *testing.T) {
	d, _, _, _ := setup(t)
	dir, err := os.MkdirTemp("", "driga-purge-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	d.Cfg.Fleet.Serve = true
	d.Cfg.Fleet.Listen = "127.0.0.1:0"
	st, err := store.Open(filepath.Join(dir, "driga.db"), store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	err = st.Tx(context.Background(), func(tx *store.Tx) error {
		remote := tx.ForMachine("remote")
		if err := remote.UpsertMachine(model.Machine{Hostname: "vm"}); err != nil {
			return err
		}
		_, err := remote.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: "remote"}})
		return err
	})
	st.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := fleet.SavePeers(filepath.Join(dir, "fleet"), []fleet.Peer{{Name: "vm", MachineID: "remote"}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, d, dir) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	for {
		if _, err := fleet.Control(ctx, dir, fleet.ControlRequest{Op: "status"}); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("collector unavailable")
		case <-time.After(time.Millisecond):
		}
	}
	if _, err := fleet.RemovePeer(ctx, dir, "vm", true); err != nil {
		t.Fatal(err)
	}
	ro, err := store.Open(filepath.Join(dir, "driga.db"), store.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	ss, err := ro.SessionsScoped(store.SessionFilter{IncludeEnded: true}, store.Scope{Machines: []string{"remote"}})
	if err != nil || len(ss) != 0 {
		t.Fatalf("remote rows %+v %v", ss, err)
	}
}
