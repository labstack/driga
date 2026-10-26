package fleet

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestSnapshotCapsArchivedToolsPerSessionAndKeepsLifecycle(t *testing.T) {
	server, st, _ := serverFixture(t)
	now := time.Date(2026, 3, 14, 15, 30, 0, 0, time.UTC)
	server.now = func() time.Time { return now }
	type key struct{ adapter, external string }
	want := map[key]int{{"claude", "shared"}: 1000, {"claude", "other"}: 1000, {"codex", "shared"}: 1000, {"gemini", "quiet"}: 7}
	err := st.Tx(context.Background(), func(tx *store.Tx) error {
		ended := now.Add(-4 * 24 * time.Hour)
		for _, group := range []struct {
			machine, adapter, external string
			calls                      int
		}{
			{st.MachineID(), "claude", "shared", 2500},
			{st.MachineID(), "claude", "other", 2507},
			{st.MachineID(), "codex", "shared", 2511},
			{st.MachineID(), "gemini", "quiet", 7},
			{"remote", "claude", "shared", 2600},
		} {
			q := tx.ForMachine(group.machine)
			if _, err := q.UpsertSession(model.Session{EndedAt: &ended, SessionObs: model.SessionObs{Adapter: group.adapter, ExternalID: group.external}}); err != nil {
				return err
			}
			// Insert directly into the archive: a raw 200-row tail must not
			// accidentally hide whether snapshot export bounds archive volume.
			for i := 0; i < group.calls; i++ {
				kind := []string{fmt.Sprintf("log:tool_call:%d", i), "PostToolUse", "PostToolUseFailure", "postToolUse", "postToolUseFailure", "otlp:tool_result"}[i%6]
				event := model.Event{Adapter: group.adapter, ExternalID: group.external, TS: now.Add(time.Duration(i-group.calls) * time.Second), Kind: kind, Metadata: &model.EventMetadata{Tool: "Read"}, Summary: "private tool contents"}
				if _, err := q.RecordPanelEvent(event); err != nil {
					return err
				}
			}
			for _, event := range []model.Event{
				{TS: now.Add(-6*24*time.Hour - time.Second), Kind: "log:turn_start"},
				{TS: now.Add(-6 * 24 * time.Hour), Kind: "log:turn_end"},
				{TS: now.Add(-8 * 24 * time.Hour), Kind: "log:turn_failed"},
			} {
				event.Adapter, event.ExternalID = group.adapter, group.external
				if _, err := q.RecordPanelEvent(event); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := server.snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	type observed struct {
		tools, lifecycle int
		oldest, latest   time.Time
	}
	got := map[key]observed{}
	for _, raw := range snap.Data.Events {
		var event model.Event
		if err := json.Unmarshal(raw, &event); err != nil {
			t.Fatal(err)
		}
		k := key{event.Adapter, event.ExternalID}
		if event.MachineID != st.MachineID() {
			t.Fatalf("snapshot leaked another machine's events: %+v", event)
		}
		if _, ok := want[k]; !ok {
			t.Fatalf("snapshot leaked an unexpected session key: %+v", event)
		}
		seen := got[k]
		if event.Metadata != nil && event.Metadata.Tool == "Read" {
			seen.tools++
			if seen.oldest.IsZero() || event.TS.Before(seen.oldest) {
				seen.oldest = event.TS
			}
			if event.TS.After(seen.latest) {
				seen.latest = event.TS
			}
		} else {
			if event.Kind != "log:turn_start" && event.Kind != "log:turn_end" {
				t.Fatalf("out-of-retention lifecycle event exported: %+v", event)
			}
			seen.lifecycle++
		}
		got[k] = seen
	}
	for k, tools := range want {
		seen := got[k]
		if seen.tools != tools || seen.lifecycle != 2 || !seen.oldest.Equal(now.Add(-time.Duration(tools)*time.Second)) || !seen.latest.Equal(now.Add(-time.Second)) {
			t.Fatalf("%+v: tools/lifecycle/window=%+v want tools=%d, lifecycle=2, newest tool rows", k, seen, tools)
		}
	}
	if len(snap.Data.Sessions) != len(want) {
		t.Fatalf("archived evidence lost session references or leaked a peer: %d sessions", len(snap.Data.Sessions))
	}
}
