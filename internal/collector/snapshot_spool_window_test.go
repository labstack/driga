package collector

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/spool"
)

func TestSnapshotDrainsOnlyRecentSpool(t *testing.T) {
	d, _, _, _ := setup(t)
	for _, age := range []time.Duration{0, 8 * 24 * time.Hour} {
		if err := spool.Append(d.SpoolDir, model.HookEvent{TS: d.Now().Add(-age), Adapter: "claude", SessionID: "one", Event: "Notification"}); err != nil {
			t.Fatal(err)
		}
	}
	st, err := Snapshot(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	events, err := st.LatestEvents(time.Time{})
	if err != nil || len(events) != 1 {
		t.Fatalf("snapshot history: %+v %v", events, err)
	}
	entries, _ := os.ReadDir(d.SpoolDir)
	if len(entries) != 2 {
		t.Fatal("snapshot pruned the spool")
	}
}
