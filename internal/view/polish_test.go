package view

import (
	"reflect"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestCwdsFreshnessUsesCollectorCadence(t *testing.T) {
	s := fixture(t, func(tx *store.Tx) error { return tx.SetSourceOK("cwds", testNow.Add(-20*time.Second)) })
	for _, age := range []time.Duration{20 * time.Second, 3 * time.Minute, 3*time.Minute + time.Second} {
		e, err := NewEnvelope(s, "daemon", testNow.Add(age-20*time.Second), collector.Cadence(0), nil)
		if err != nil {
			t.Fatal(err)
		}
		if e.Freshness["cwds"].Stale != (age > 3*time.Minute) {
			t.Errorf("age %s: %+v", age, e.Freshness["cwds"])
		}
	}
}

func TestOTLPNotYetReceiving(t *testing.T) {
	for _, failure := range []bool{false, true} {
		s := fixture(t, func(tx *store.Tx) error {
			if err := tx.SetSourceOK("otlp", time.Time{}); err != nil {
				return err
			}
			if failure {
				return tx.SetSourceError("otlp", "listen failed", testNow)
			}
			return nil
		})
		e, err := NewEnvelope(s, "daemon", testNow, collector.Cadence(0), nil)
		if err != nil {
			t.Fatal(err)
		}
		if !failure && e.Freshness["otlp"].Status != "not-yet-receiving" {
			t.Errorf("missing pending status: %+v", e.Freshness["otlp"])
		}
		if e.Freshness["otlp"].Stale != failure {
			t.Errorf("failure=%v: %+v", failure, e.Freshness["otlp"])
		}
	}
}

func TestGaugeLevels(t *testing.T) {
	for _, tc := range []struct {
		used, total uint64
		level       string
		percent     float64
	}{{0, 0, "normal", 0}, {0, 100, "normal", 0}, {74, 100, "normal", 74}, {75, 100, "warning", 75}, {89, 100, "warning", 89}, {90, 100, "critical", 90}, {100, 100, "critical", 100}, {200, 100, "critical", 100}} {
		if got := GaugeLevel(tc.used, tc.total); got != tc.level {
			t.Errorf("%d/%d: %s, want %s", tc.used, tc.total, got, tc.level)
		}
		if got := GaugePercent(tc.used, tc.total); got != tc.percent {
			t.Errorf("percent: %v want %v", got, tc.percent)
		}
	}
	for _, tc := range []struct {
		used  uint64
		state string
	}{{20, "working"}, {75, "waiting"}, {95, "stalled?"}} {
		if !reflect.DeepEqual(GaugeStyle(tc.used, 100, false).GetForeground(), StateStyle(tc.state, false).GetForeground()) {
			t.Error("gauge color differs from state color")
		}
	}
}

func TestIdleIsExplicitBlueAndNoColor(t *testing.T) {
	for _, state := range []string{"idle", "~idle"} {
		if !reflect.DeepEqual(StateStyle(state, false).GetForeground(), lipgloss.Color("#5fafff")) {
			t.Error("idle must use explicit blue, independent of terminal palette")
		}
		if StateStyle(state, true).Render("idle") != "idle" {
			t.Error("NO_COLOR ignored")
		}
	}
}

func TestStateCountsOrder(t *testing.T) {
	got := StateCounts([]AgentRow{{Label: "~idle"}, {Label: "working"}, {Label: "idle"}, {Label: "waiting"}})
	want := []StateCount{{Label: "working", Count: 1}, {Label: "waiting", Count: 1}, {Label: "idle", Count: 2}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v want %+v", got, want)
	}
}

func TestNeedsYouNewestPerMachineSession(t *testing.T) {
	now := testNow
	inbox := []InboxRow{
		{MachineID: "a", InboxItem: model.InboxItem{ID: "old", SessionID: 1, Kind: "finished", TS: now.Add(-time.Minute)}},
		{MachineID: "a", InboxItem: model.InboxItem{ID: "new", SessionID: 1, Kind: "waiting", TS: now}},
		{MachineID: "a", InboxItem: model.InboxItem{ID: "middle", SessionID: 1, Kind: "worker-finished", TS: now.Add(-time.Second)}},
		{MachineID: "b", InboxItem: model.InboxItem{ID: "peer", SessionID: 1, Kind: "waiting", TS: now}},
		{MachineID: "a", InboxItem: model.InboxItem{ID: "acked", SessionID: 1, Kind: "waiting", TS: now.Add(time.Minute), AckedAt: &now}},
	}
	got := NeedsYou(inbox, nil, nil)
	if len(got) != 2 || got[0].Inbox.ID != "new" || got[1].Inbox.ID != "peer" {
		t.Fatalf("%+v", got)
	}
}

func TestProjectsLiveThenRecentThenQuiet(t *testing.T) {
	rows := []ProjectRow{
		{Status: ProjectStatus{Repo: model.Repo{Name: "quiet"}, Activity: "idle"}},
		{Status: ProjectStatus{Repo: model.Repo{Name: "older"}, Where: []WherePart{{At: testNow.Add(-time.Hour)}}}},
		{Status: ProjectStatus{Repo: model.Repo{Name: "newer"}, Where: []WherePart{{At: testNow}}}},
		{Status: ProjectStatus{Repo: model.Repo{Name: "live"}, LiveAgents: 1}},
	}
	got := SortedProjects(rows)
	for i, name := range []string{"live", "newer", "older", "quiet"} {
		if got[i].Status.Repo.Name != name {
			t.Fatalf("order: %+v", got)
		}
	}
	if rows[0].Status.Repo.Name != "quiet" {
		t.Fatal("sort mutated snapshot")
	}
}

func TestNeedsYouTitleFallsBackToProject(t *testing.T) {
	rows := NeedsYou([]InboxRow{{MachineID: "local", Project: "repo", InboxItem: model.InboxItem{ID: "event", SessionID: 1, Kind: "waiting"}}}, []AgentRow{{MachineID: "local", ID: 1, Title: "Last prompt is not a title", Adapter: "claude", Project: "repo"}}, map[int64]AgentView{1: {MachineID: "local", Session: model.Session{SessionObs: model.SessionObs{Adapter: "claude"}}}})
	if len(rows) != 1 || rows[0].Title != "repo" {
		t.Fatalf("title must fall back to project: %+v", rows)
	}
}
