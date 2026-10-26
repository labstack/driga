package view

import (
	"context"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"reflect"
	"testing"
	"time"
)

func TestFleetPanelDetailsAndHeatmap(t *testing.T) {
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Date(2026, 3, 14, 15, 30, 0, 0, time.UTC)
	var id int64
	err = st.Tx(context.Background(), func(tx *store.Tx) error {
		if err := tx.UpsertMachine(model.Machine{Hostname: "studio"}); err != nil {
			return err
		}
		repo, err := tx.UpsertRepo(model.Repo{Path: "/src/atlas", Name: "atlas"})
		if err != nil {
			return err
		}
		id, err = tx.UpsertSession(model.Session{RepoID: &repo, Label: "working", SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "one", Cwd: "/src/atlas", StartedAt: now.Add(-time.Hour)}})
		if err != nil {
			return err
		}
		if err := tx.ReplaceSessionFiles(id, []model.FileTouch{{Path: "/src/atlas/main.go", TS: now}, {Path: "/secret/outside", TS: now}}); err != nil {
			return err
		}
		events := []struct {
			kind string
			at   time.Time
		}{{"UserPromptSubmit", now.Add(-10 * time.Minute)}, {"PostToolUse", now.Add(-9 * time.Minute)}, {"otlp:tool_result", now.Add(-9 * time.Minute)}, {"Stop", now.Add(-8 * time.Minute)}, {"UserPromptSubmit", now.Add(-7 * time.Minute)}, {"PostToolUseFailure", now.Add(-6 * time.Minute)}, {"Stop", now.Add(-5 * time.Minute)}, {"UserPromptSubmit", now.Add(-4 * time.Minute)}, {"errorOccurred", now.Add(-3 * time.Minute)}, {"PostToolUse", now.Add(time.Hour)}, {"PostToolUse", now.Add(-25 * time.Hour)}}
		for _, e := range events {
			if err := tx.InsertEvent(model.Event{Adapter: "claude", ExternalID: "one", TS: e.at, Kind: e.kind}); err != nil {
				return err
			}
		}
		end := now.Add(-8 * time.Minute)
		return tx.UpsertInboxItem(model.InboxItem{ID: "wait", SessionID: id, Kind: "waiting", TS: now.Add(-9 * time.Minute), EndedAt: &end})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, capture := range []string{"none", "titles", "snippets"} {
		details, hours, err := FleetPanels(st, Filter{CaptureText: capture}, now)
		if err != nil {
			t.Fatal(err)
		}
		if len(details) != 1 || !reflect.DeepEqual(details[0].Turns, []string{"waited", "ok", "failed"}) || !reflect.DeepEqual(details[0].Files, []string{"main.go"}) {
			t.Fatalf("%s: %+v", capture, details)
		}
		if len(hours) != 24 || hours[23].Calls != 2 || hours[0].TS != now.UTC().Truncate(time.Hour).Add(-23*time.Hour) {
			t.Fatalf("heatmap: %+v", hours)
		}
	}
	_, hours, err := FleetPanels(st, Filter{Scope: store.Scope{Machines: []string{"other"}}}, now)
	if err != nil || len(hours) != 0 {
		t.Fatalf("scope leaked: %v %v", hours, err)
	}
}

func TestTurnOutcomesLast30(t *testing.T) {
	now := time.Date(2026, 3, 14, 15, 0, 0, 0, time.UTC)
	var events []model.Event
	for i := 0; i < 40; i++ {
		events = append(events, model.Event{Kind: "log:turn_end", TS: now.Add(time.Duration(i-40) * time.Minute)})
	}
	got := turnOutcomes(events, nil, 1, now)
	if len(got) != 30 {
		t.Fatalf("turns = %d", len(got))
	}
	for _, outcome := range got {
		if outcome != "ok" {
			t.Fatal(got)
		}
	}
}

func TestPanelHistorySurvivesReads(t *testing.T) {
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Date(2026, 3, 14, 15, 0, 30, 0, time.UTC)
	err = st.Tx(context.Background(), func(tx *store.Tx) error {
		if err := tx.UpsertMachine(model.Machine{Hostname: "studio"}); err != nil {
			return err
		}
		_, err := tx.UpsertSession(model.Session{Label: "stalled?", SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "one"}})
		if err != nil {
			return err
		}
		for i := 0; i < 30; i++ {
			if err := tx.RecordPanelPolls(now.Add(time.Duration(i-29) * time.Minute)); err != nil {
				return err
			}
		}
		ack := now.Add(-10 * time.Minute)
		for _, item := range []model.InboxItem{{ID: "w", SessionID: 1, Kind: "waiting", TS: now.Add(-40 * time.Minute), AckedAt: &ack}, {ID: "f1", SessionID: 1, Kind: "finished", TS: now.Add(-30 * time.Minute)}, {ID: "f2", SessionID: 1, Kind: "finished", TS: now.Add(-20 * time.Minute)}} {
			if err := tx.UpsertInboxItem(item); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := PanelHistory(st, Filter{}, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := PanelHistory(st, Filter{}, now)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("reads differ %v", err)
	}
	if len(first.Attention) != 60 || len(first.Polls) != 30 {
		t.Fatalf("history: %+v", first)
	}
	if first.Attention[20].Waiting != 1 || first.Attention[59].Waiting != 0 || first.Attention[59].Review != 1 || first.Attention[59].Stuck == nil || *first.Attention[59].Stuck != 1 {
		t.Fatalf("attention: %+v", first.Attention)
	}
	if first.Attention[0].Stuck != nil {
		t.Fatal("invented stuck history")
	}
}

func TestTurnChannelsDoNotDuplicateOrMergeFastSeparateTurns(t *testing.T) {
	now := time.Date(2026, 3, 14, 15, 0, 0, 0, time.UTC)
	events := []model.Event{{TS: now.Add(-4 * time.Second), Kind: "log:turn_end"}, {TS: now.Add(-3 * time.Second), Kind: "Stop"}, {TS: now.Add(-2 * time.Second), Kind: "log:turn_start"}, {TS: now.Add(-time.Second), Kind: "log:turn_end"}, {TS: now, Kind: "Stop"}}
	got := turnOutcomes(events, nil, 1, now)
	if !reflect.DeepEqual(got, []string{"ok", "ok"}) {
		t.Fatal(got)
	}
}

func TestPanelHistoryDoesNotSumUnknownPeerStuckCounts(t *testing.T) {
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now().UTC()
	err = st.Tx(context.Background(), func(tx *store.Tx) error {
		if err := tx.UpsertMachine(model.Machine{Hostname: "studio"}); err != nil {
			return err
		}
		if err := tx.RecordPanelPolls(now); err != nil {
			return err
		}
		return tx.ForMachine("peer").UpsertMachine(model.Machine{MachineID: "peer", Hostname: "build", OS: "linux"})
	})
	if err != nil {
		t.Fatal(err)
	}
	history, err := PanelHistory(st, Filter{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if history.Attention[59].Stuck != nil {
		t.Fatal("partial fleet history was presented as a known total")
	}
}
