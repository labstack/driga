package store

import (
	"context"
	"encoding/json"
	"github.com/labstack/driga/internal/feed"
	"github.com/labstack/driga/internal/model"
	"path/filepath"
	"testing"
	"time"
)

func TestPanelSampleRoundTrip(t *testing.T) {
	st := openTest(t, "", Memory)
	at := time.Date(2026, 3, 14, 15, 0, 0, 0, time.UTC)
	cpu := 37.5
	sample := model.MachineSample{TS: at, DiskTotal: 500, DiskFree: 100, CPUPercent: &cpu}
	mustTx(t, st, func(tx *Tx) error { return tx.InsertSample(sample) })
	got, err := st.LatestSample()
	if err != nil || got.DiskTotal != 500 || got.CPUPercent == nil || *got.CPUPercent != cpu {
		t.Fatalf("stored sample: %+v %v", got, err)
	}
	_, wire := feed.Encode(feed.MachineSample, got, feed.None)
	decoded, err := feed.Decode(feed.MachineSample, wire)
	if err != nil {
		t.Fatal(err)
	}
	remote := decoded.(*model.MachineSample)
	if remote.DiskTotal != 500 || remote.CPUPercent == nil || *remote.CPUPercent != cpu {
		t.Fatalf("feed lost sample: %s", wire)
	}
	sample.CPUPercent = nil
	mustTx(t, st, func(tx *Tx) error { return tx.InsertSample(sample) })
	got, err = st.LatestSample()
	if err != nil || got.CPUPercent != nil {
		t.Fatalf("unknown CPU must stay unknown: %+v %v", got, err)
	}
	var v map[string]any
	b, _ := json.Marshal(got)
	_ = json.Unmarshal(b, &v)
	if _, ok := v["cpu_percent"]; !ok || v["cpu_percent"] != nil {
		t.Fatal(string(b))
	}
}

func TestPanelPollRetention(t *testing.T) {
	st := openTest(t, "", Memory)
	now := time.Date(2026, 3, 14, 15, 0, 0, 0, time.UTC)
	mustTx(t, st, func(tx *Tx) error {
		if err := tx.UpsertMachine(model.Machine{Hostname: "studio"}); err != nil {
			return err
		}
		id, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "one"}, Label: "stalled?"})
		if err != nil {
			return err
		}
		_ = id
		for i := 0; i < 40; i++ {
			if err := tx.RecordPanelPolls(now.Add(time.Duration(i-40) * time.Hour)); err != nil {
				return err
			}
		}
		return tx.RecordPanelPolls(now)
	})
	polls, err := st.PanelPolls(now)
	if err != nil || len(polls) != 30 {
		t.Fatalf("polls %d: %v", len(polls), err)
	}
	if polls[29].Worst != "stuck" || polls[29].Stuck != 1 {
		t.Fatal(polls[29])
	}
}

func TestPanelEventsOutliveRawTailAndReplay(t *testing.T) {
	st := openTest(t, "", Memory)
	now := time.Date(2026, 3, 14, 15, 0, 0, 0, time.UTC)
	mustTx(t, st, func(tx *Tx) error {
		for i := 0; i < 350; i++ {
			e := model.Event{Adapter: "claude", ExternalID: "one", TS: now.Add(time.Duration(i-350) * time.Second), Kind: "PostToolUse", Summary: "private contents"}
			if err := tx.InsertEvent(e); err != nil {
				return err
			}
		}
		// An old raw event was evicted; replay must not inflate the panel count.
		return tx.ApplyEvent(model.Event{Adapter: "claude", ExternalID: "one", TS: now.Add(-350 * time.Second), Kind: "PostToolUse"})
	})
	raw, err := st.Events("claude", "one", time.Time{})
	if err != nil || len(raw) != 200 {
		t.Fatal(len(raw), err)
	}
	events, err := st.PanelEvents("claude", "one", time.Time{})
	if err != nil || len(events) != 350 {
		t.Fatal(len(events), err)
	}
	for _, e := range events {
		if e.Summary != "" {
			t.Fatal("panel retained content")
		}
	}
}

func TestPanelEventPublishesOnce(t *testing.T) {
	st := openTest(t, "", Memory)
	e := model.Event{Adapter: "gemini", ExternalID: "one", TS: time.Now(), Kind: "log:turn_end"}
	mustTx(t, st, func(tx *Tx) error {
		first, err := tx.RecordPanelEvent(e)
		if err != nil {
			return err
		}
		second, err := tx.RecordPanelEvent(e)
		if !first || second {
			t.Fatalf("new flags: %v %v", first, second)
		}
		return err
	})
}

func TestPanelHistoryPersistsAcrossStoreReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "driga.db")
	now := time.Now().UTC()
	st, err := Open(path, ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	err = st.Tx(context.Background(), func(tx *Tx) error {
		if err := tx.UpsertMachine(model.Machine{Hostname: "studio"}); err != nil {
			return err
		}
		return tx.RecordPanelPolls(now)
	})
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	reopened, err := Open(path, ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	polls, err := reopened.PanelPolls(now)
	if err != nil || len(polls) != 1 || polls[0].Worst != "quiet" {
		t.Fatalf("reopened history %+v %v", polls, err)
	}
}
