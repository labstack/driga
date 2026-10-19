package collector

import (
	"context"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/spool"
	"github.com/labstack/driga/internal/store"
	"os"
	"path/filepath"
	"testing"
)

func TestCollectorSpoolMigrationRetainsStoredOffsets(t *testing.T) {
	d, st, _, _ := setup(t)
	t.Setenv("DRIGA_SPOOL_DIR", "")
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	d.SpoolDir = config.SpoolDir()
	old := config.LegacySpoolDir()
	ev := model.HookEvent{TS: d.now(), Event: "Stop", Adapter: "claude", SessionID: "old"}
	if err := spool.Append(old, ev); err != nil {
		t.Fatal(err)
	}
	_, offsets, err := spool.Drain(old, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		for name, o := range offsets {
			if err := tx.SetOffset("spool/"+name, o); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ev.SessionID = "pending"
	if err := spool.Append(old, ev); err != nil {
		t.Fatal(err)
	}
	ev.SessionID = "new"
	if err := spool.Append(d.SpoolDir, ev); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := MigrateSpool(context.Background(), d, st); err != nil {
			t.Fatal(err)
		}
		pass(t, d, st, Plan{})
	}
	events, err := st.LatestEvents(d.now().Add(-1))
	if err != nil || len(events) != 2 {
		t.Fatal(events, err)
	}
	for _, e := range events {
		if e.ExternalID == "old" {
			t.Fatal("replayed consumed legacy record")
		}
	}
	if files, _ := filepath.Glob(filepath.Join(old, "*.jsonl")); len(files) != 0 {
		t.Fatal(files)
	}
}

func TestCollectorSpoolOverrideDoesNotMigrate(t *testing.T) {
	d, st, _, _ := setup(t)
	old := config.LegacySpoolDir()
	if err := spool.Append(old, model.HookEvent{TS: d.now(), Event: "Stop"}); err != nil {
		t.Fatal(err)
	}
	if err := MigrateSpool(context.Background(), d, st); err != nil {
		t.Fatal(err)
	}
	if files, _ := filepath.Glob(filepath.Join(old, "*.jsonl")); len(files) != 1 {
		t.Fatal(files)
	}
	if _, err := os.Stat(filepath.Join(old, "README")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
