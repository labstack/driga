package view

import (
	"context"
	"errors"
	"fmt"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"reflect"
	"testing"
	"time"
)

type panelBatchReader struct {
	Reader
	batches, individual int
}

func (r *panelBatchReader) AllPanelEvents(since time.Time) ([]model.Event, error) {
	r.batches++
	return r.Reader.AllPanelEvents(since)
}

func (r *panelBatchReader) PanelEvents(string, string, time.Time) ([]model.Event, error) {
	r.individual++
	return nil, errors.New("per-session panel evidence query")
}

func TestFleetPanelsBatchSessionEvidence(t *testing.T) {
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Date(2026, 3, 14, 15, 30, 0, 0, time.UTC)
	waited := int64(0)
	err = st.Tx(context.Background(), func(tx *store.Tx) error {
		repo, err := tx.UpsertRepo(model.Repo{Path: "/src/main", Name: "Project"})
		if err != nil {
			return err
		}
		worktree, err := tx.UpsertWorktree(model.Worktree{RepoID: repo, Path: "/src/branch"})
		if err != nil {
			return err
		}
		for i := 0; i < 200; i++ {
			adapter, ending := "claude", "log:turn_failed"
			if i%2 == 1 {
				adapter, ending = "codex", "log:turn_end"
			}
			external := fmt.Sprint(i / 2) // The adapters deliberately share external IDs.
			id, err := tx.UpsertSession(model.Session{RepoID: &repo, WorktreeID: &worktree, SessionObs: model.SessionObs{Adapter: adapter, ExternalID: external, Cwd: "/src/branch", StartedAt: now.Add(-time.Hour)}})
			if err != nil {
				return err
			}
			for _, event := range []model.Event{
				{Adapter: adapter, ExternalID: external, TS: now.Add(-time.Minute), Kind: "log:turn_start"},
				{Adapter: adapter, ExternalID: external, TS: now, Kind: ending},
				{Adapter: adapter, ExternalID: external, TS: now, Kind: "log:tool_call:one"},
				{Adapter: adapter, ExternalID: external, TS: now, Kind: "PostToolUse"},
			} {
				if _, err := tx.RecordPanelEvent(event); err != nil {
					return err
				}
			}
			if err := tx.ReplaceSessionFiles(id, []model.FileTouch{{Path: "/src/branch/main.go", TS: now}, {Path: "/outside/main.go", TS: now}}); err != nil {
				return err
			}
			if i == 1 {
				waited = id
				if err := tx.UpsertInboxItem(model.InboxItem{ID: "one-wait", SessionID: id, Kind: "waiting", TS: now.Add(-time.Second)}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	r := &panelBatchReader{Reader: st}
	details, hours, err := fleetMachinePanels(r, model.Machine{MachineID: st.MachineID()}, Filter{}, now)
	if err != nil || r.batches != 1 || r.individual != 0 || len(details) != 200 {
		t.Fatalf("not one evidence batch: batch=%d individual=%d details=%d err=%v", r.batches, r.individual, len(details), err)
	}
	for _, d := range details {
		s, err := st.Session(d.ID)
		if err != nil {
			t.Fatal(err)
		}
		outcome := "ok"
		if s.Adapter == "claude" {
			outcome = "failed"
		} else if d.ID == waited {
			outcome = "waited"
		}
		if !reflect.DeepEqual(d.Turns, []string{outcome}) || !reflect.DeepEqual(d.Files, []string{"main.go"}) {
			t.Fatalf("indexed joins mixed sessions or lost worktree-relative paths: %+v", d)
		}
	}
	if len(hours) != 24 || hours[23].Calls != 200 {
		t.Fatalf("reporting channels were added instead of deduplicated: %+v", hours)
	}
}

func TestFleetPanelsIncludeLiveAndRecentlyEndedSessions(t *testing.T) {
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Date(2026, 3, 14, 15, 30, 0, 0, time.UTC)
	recent, old := now.Add(-time.Hour), now.Add(-25*time.Hour)
	err = st.Tx(context.Background(), func(tx *store.Tx) error {
		for i, end := range []*time.Time{nil, &recent, &old} {
			if _, err := tx.UpsertSession(model.Session{EndedAt: end, SessionObs: model.SessionObs{Adapter: "claude", ExternalID: fmt.Sprint(i), StartedAt: now.Add(-14 * 24 * time.Hour)}}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	details, _, err := FleetPanels(st, Filter{}, now)
	if err != nil || len(details) != 2 {
		t.Fatalf("live or recently ended session was excluded: %+v %v", details, err)
	}
}

func TestTurnOutcomesIndexedWaitBoundaries(t *testing.T) {
	now := time.Date(2026, 3, 14, 15, 30, 0, 0, time.UTC)
	first, second := now.Add(-3*time.Second), now.Add(-time.Second)
	events := []model.Event{
		{TS: now.Add(-4 * time.Second), Kind: "log:turn_start"},
		{TS: first, Kind: "log:turn_end"},
		{TS: first.Add(time.Millisecond), Kind: "Stop"},
		{TS: now.Add(-2 * time.Second), Kind: "UserPromptSubmit"},
		{TS: second, Kind: "log:turn_end"},
		{TS: second.Add(time.Millisecond), Kind: "Stop"},
	}
	// Deliberately unsorted, with a different session's wait and a future wait.
	inbox := []model.InboxItem{
		{SessionID: 1, Kind: "waiting", TS: now.Add(time.Hour)},
		{SessionID: 2, Kind: "waiting", TS: second},
		{SessionID: 1, Kind: "waiting", TS: first},
	}
	if got := turnOutcomes(events, inbox, 1, now); !reflect.DeepEqual(got, []string{"waited", "ok"}) {
		t.Fatalf("wait at one endpoint leaked into the next turn: %v", got)
	}
	inbox = append(inbox, model.InboxItem{SessionID: 1, Kind: "waiting", TS: second})
	if got := turnOutcomes(events, inbox, 1, now); !reflect.DeepEqual(got, []string{"waited", "waited"}) {
		t.Fatalf("wait at the current endpoint was lost: %v", got)
	}
}

func panelTurnHistory(turns int) ([]model.Event, []model.InboxItem, time.Time) {
	start := time.Date(2026, 3, 14, 0, 0, 0, 0, time.UTC)
	events := make([]model.Event, 0, 3*turns)
	var inbox []model.InboxItem
	for i := 0; i < turns; i++ {
		at := start.Add(time.Duration(i) * 4 * time.Second)
		events = append(events, model.Event{TS: at, Kind: "log:turn_start"}, model.Event{TS: at.Add(time.Second), Kind: "log:turn_end"}, model.Event{TS: at.Add(2 * time.Second), Kind: "Stop"})
		if i%3 == 0 {
			inbox = append(inbox, model.InboxItem{SessionID: 1, Kind: "waiting", TS: at.Add(time.Second)})
		}
	}
	return events, inbox, start.Add(time.Duration(turns) * 4 * time.Second)
}

func BenchmarkTurnOutcomesRetainedHistory(b *testing.B) {
	events, inbox, now := panelTurnHistory(12000)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		out := turnOutcomes(events, inbox, 1, now)
		if len(out) != 30 || out[len(out)-1] != "ok" {
			b.Fatal("lost retained turn outcomes", out)
		}
	}
}

func TestTurnOutcomesRetainedHistoryBudget(t *testing.T) {
	events, inbox, now := panelTurnHistory(12000)
	started := time.Now()
	out := turnOutcomes(events, inbox, 1, now)
	// Indexed lookups take a few milliseconds for this retained history. Keep
	// ample room for race instrumentation and slower CI while catching a full
	// history scan for every turn endpoint.
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("turn outcomes repeatedly scanned retained history: %s", elapsed)
	}
	if len(out) != 30 || out[len(out)-1] != "ok" {
		t.Fatal("lost retained turn outcomes", out)
	}
}

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
		_, err := tx.UpsertSession(model.Session{Label: "stuck", SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "one"}})
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
