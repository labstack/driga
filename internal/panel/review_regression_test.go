package panel

import (
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
	"slices"
	"testing"
	"time"
)

func TestHeaderCountsWaitingWithoutInboxAndDeduplicatesApprovals(t *testing.T) {
	s := snapshot()
	s.LocalMachineID = "a"
	s.Inbox.Rows = nil
	s.Agents.Rows = []view.AgentRow{
		{MachineID: "a", ID: 1, Label: "waiting", Children: []view.AgentRow{{MachineID: "a", ID: 2, Label: "~waiting"}}},
		{MachineID: "b", ID: 1, Label: "waiting"},
	}
	f, err := Run(s, RunRequest{Dashboard: Header()})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Results[0].Frame.Rows[0][0]; got != 3.0 {
		t.Fatalf("Need you = %v, want 3", got)
	}
	if !slices.Equal(f.WaitingSessions, []int64{1, 2}) {
		t.Fatalf("local sessions already counted = %v", f.WaitingSessions)
	}
}

func TestSessionPanelsAndWaitsUseMachineIdentity(t *testing.T) {
	s := snapshot()
	s.Agents.Rows = []view.AgentRow{{ID: 11, MachineID: "a", Title: "A"}, {ID: 11, MachineID: "b", Title: "B"}}
	s.SessionPanels = []view.SessionPanel{{ID: 11, MachineID: "a", Turns: []string{"ok"}, Files: []string{"a.go"}}, {ID: 11, MachineID: "b", Turns: []string{"failed"}, Files: []string{"b.go", "b2.go"}}}
	rows := sources["sessions"].build(s)
	if rows[0][14] != "ok" || rows[0][15] != float64(1) || rows[0][16] != `["a.go"]` || rows[1][14] != "failed" || rows[1][15] != float64(2) || rows[1][16] != `["b.go","b2.go"]` {
		t.Fatal(rows)
	}
	now := s.ObservedAt
	s.BoardData.Nodes = []view.FlowNode{{ID: 11, MachineID: "a", StartedAt: now.Add(-time.Hour), Waits: []view.FlowWait{{StartedAt: now.Add(-time.Minute)}}}, {ID: 11, MachineID: "b", StartedAt: now.Add(-time.Hour), Waits: []view.FlowWait{{StartedAt: now.Add(-2 * time.Minute)}}}}
	f, err := Run(s, RunRequest{Dashboard: Dashboard{Name: "Timeline", Panels: []Panel{{ID: "t", Title: "T", Viz: "timeline", Source: "flow", Where: []Filter{{Column: "machine_id", Value: "a"}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	spans := f.Results[0].Frame.Spans
	if len(spans) != 1 || spans[0].MachineID != "a" {
		t.Fatal(spans)
	}
	s.LocalMachineID = "a"
	s.Machine.SessionCosts = []view.SessionCost{{SessionID: 11}}
	cost := sources["costs"].build(s)
	if cost[0][6] != "A" {
		t.Fatal(cost)
	}
}
func TestListGroupingPreservesRowsAndFilterTrimsMembership(t *testing.T) {
	s := snapshot()
	for _, viz := range []string{"list", "cards", "inbox"} {
		source := "sessions"
		if viz == "inbox" {
			source = "inbox"
		}
		f, err := Run(s, RunRequest{Dashboard: Dashboard{Name: "Rows", Panels: []Panel{{ID: "r", Title: "R", Viz: viz, Source: source, By: "project"}}}})
		if err != nil {
			t.Fatal(viz, err)
		}
		if f.Results[0].Frame.Columns[0].Name == "project" && len(f.Results[0].Frame.Columns) == 2 {
			t.Fatalf("%s collapsed rows", viz)
		}
	}
	if !match("codex", "in", "claude, codex") {
		t.Fatal("membership whitespace rejected")
	}
}
func TestMissingMachineSampleRemainsUnknown(t *testing.T) {
	s := snapshot()
	s.Machines.Rows = []view.MachineRow{{Machine: model.Machine{MachineID: "peer"}}}
	r := sources["machines"].build(s)[0]
	for _, i := range []int{7, 8, 9, 10, 11} {
		if r[i] != nil {
			t.Fatalf("missing sample invented metric %d=%v", i, r[i])
		}
	}
}
func TestHeaderMemoryUsesOneHost(t *testing.T) {
	s := snapshot()
	s.Summary.MachineID = "here"
	s.Activity.Machines[0].MachineID = "here"
	s.Activity.Machines[0].Buckets[0].Sample.MemTotal = 20
	s.Activity.Machines = append(s.Activity.Machines, view.ActivityMachine{MachineID: "peer", Buckets: []view.ActivityBucket{{TS: s.ObservedAt, Sample: &model.MachineSample{MemUsed: 500, MemTotal: 1000}}}})
	f, err := Run(s, RunRequest{Dashboard: Header()})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range f.Results {
		if r.ID == "memory" && (r.Frame.Rows[0][0] != float64(10) || r.Total != float64(20)) {
			t.Fatalf("mixed hosts %+v", r)
		}
	}
}

func TestHeaderLeftoverCountIsNotPaginated(t *testing.T) {
	s := snapshot()
	s.Orphans.Rows = make([]view.OrphanRow, 201)
	f, err := Run(s, RunRequest{Dashboard: Header()})
	if err != nil || f.LeftoverCount != 201 {
		t.Fatalf("count %d %v", f.LeftoverCount, err)
	}
}
