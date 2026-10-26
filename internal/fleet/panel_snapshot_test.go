package fleet

import (
	"context"
	"encoding/json"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"testing"
	"time"
)

func TestSnapshotPreservesPanelMetadataAndResources(t *testing.T) {
	server, st, _ := serverFixture(t)
	now := time.Now().UTC()
	server.now = func() time.Time { return now }
	cpu := 25.0
	old := now.Add(-3 * 24 * time.Hour)
	err := st.Tx(context.Background(), func(tx *store.Tx) error {
		if _, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "codex", ExternalID: "metadata-only"}, EndedAt: &old}); err != nil {
			return err
		}
		// A metadata-only session must be included even when older than the live window.
		if err := tx.InsertEvent(model.Event{Adapter: "codex", ExternalID: "metadata-only", TS: old, Kind: "log:turn_end", Summary: "private contents"}); err != nil {
			return err
		}
		for i := 0; i < 350; i++ {
			if err := tx.InsertEvent(model.Event{Adapter: "claude", ExternalID: "busy", TS: now.Add(time.Duration(i-350) * time.Second), Kind: "PostToolUse", Summary: "private contents"}); err != nil {
				return err
			}
		}
		return tx.InsertSample(model.MachineSample{TS: now, CPUPercent: &cpu, DiskTotal: 500, DiskFree: 100})
	})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := server.snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Data.Events) != 351 {
		t.Fatalf("metadata beyond the raw tail lost: %d", len(snap.Data.Events))
	}
	if len(snap.Data.Sessions) != 1 {
		t.Fatalf("metadata session reference lost: %d", len(snap.Data.Sessions))
	}
	var sample model.MachineSample
	if err := json.Unmarshal(snap.Data.MachineSample, &sample); err != nil || sample.DiskTotal != 500 || sample.CPUPercent == nil || *sample.CPUPercent != cpu {
		t.Fatalf("sample %+v %v", sample, err)
	}
	for _, raw := range snap.Data.Events {
		var e model.Event
		if err := json.Unmarshal(raw, &e); err != nil {
			t.Fatal(err)
		}
		if e.Kind == "log:turn_end" && e.Summary != "" {
			t.Fatal("metadata published content")
		}
	}
}
