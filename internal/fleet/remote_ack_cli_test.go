package fleet_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/cli"
	"github.com/labstack/driga/internal/fleet"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/spool"
	"github.com/labstack/driga/internal/store"
)

func TestRemoteAckWithoutPermission(t *testing.T) {
	dir := t.TempDir()
	spoolDir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("DRIGA_DATA_DIR", dir)
	t.Setenv("DRIGA_SPOOL_DIR", spoolDir)
	t.Setenv("DRIGA_CONFIG", filepath.Join(t.TempDir(), "config.toml"))
	st, err := store.Open(filepath.Join(dir, "driga.db"), store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		if err := tx.ForMachine("remote").UpsertMachine(model.Machine{Name: "vm-1"}); err != nil {
			return err
		}
		return tx.ForMachine("remote").UpsertInboxItem(model.InboxItem{ID: "item", TS: time.Now()})
	}); err != nil {
		t.Fatal(err)
	}
	peer := fleet.Peer{Name: "vm-1", MachineID: "remote"}
	if err := fleet.SavePeers(filepath.Join(dir, "fleet"), []fleet.Peer{peer}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = (cli.InboxAckCmd{ID: "item"}).Run(&cli.Globals{Stdout: &out, Machine: "vm-1"})
	if err == nil || err.Error() != "no ack permission on vm-1 (re-pair with --ack)" {
		t.Fatalf("unexpected permission error: %v", err)
	}
	events, _, err := spool.Drain(spoolDir, nil)
	if err != nil || len(events) != 0 {
		t.Fatalf("unauthorized spool: %+v %v", events, err)
	}
	// The same command routes an allowed peer without making a network call.
	json.Unmarshal([]byte(`{"name":"vm-1","machine_id":"remote","ack":true}`), &peer)
	if err := fleet.SavePeers(filepath.Join(dir, "fleet"), []fleet.Peer{peer}); err != nil {
		t.Fatal(err)
	}
	if err := (cli.InboxAckCmd{ID: "item"}).Run(&cli.Globals{Stdout: &out, Machine: "vm-1"}); err != nil {
		t.Fatal(err)
	}
	events, _, err = spool.Drain(spoolDir, nil)
	encoded, _ := json.Marshal(events)
	if err != nil || len(events) != 1 || !strings.Contains(string(encoded), `"machine_id":"remote"`) {
		t.Fatalf("remote route: %s %v", encoded, err)
	}
}
