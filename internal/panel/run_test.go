package panel

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
	"github.com/labstack/driga/internal/viewer"
)

func snapshot() viewer.Snapshot {
	now := time.Date(2026, 10, 25, 14, 0, 0, 0, time.UTC)
	s := viewer.Snapshot{LocalMachineID: "here"}
	s.ObservedAt = now
	local := view.MachineFresh{MachineName: "laptop"}
	s.Agents.Rows = []view.AgentRow{
		{ID: 1, MachineID: "here", Title: "fix tz", Label: "working", Adapter: "claude", Project: "core", MachineFresh: local, Mem: 300, Children: []view.AgentRow{{ID: 2, Title: "worker", Label: "waiting", Adapter: "claude", Project: "core", MachineFresh: local, Mem: 100}}},
		{ID: 3, Title: "docs", Label: "working", Adapter: "codex", Project: "site", MachineFresh: local, Mem: 50},
	}
	s.Inbox.Rows = []view.InboxRow{
		{InboxItem: model.InboxItem{ID: "done", Kind: "finished", SessionID: 3}, Project: "site", MachineFresh: local, Age: time.Minute},
		{InboxItem: model.InboxItem{ID: "short", Kind: "waiting", SessionID: 2, Tool: "Bash"}, Project: "core", MachineFresh: local, Waited: time.Minute},
		{InboxItem: model.InboxItem{ID: "long", Kind: "waiting", SessionID: 1}, Project: "core", MachineFresh: local, Waited: time.Hour},
		{InboxItem: model.InboxItem{ID: "old", Kind: "waiting"}, Historical: true},
	}
	s.Cleanup = []cleanup.Candidate{{ID: "wt", Kind: "worktree", Path: "/src/core-wt", Status: "ready", Size: 2048}}
	s.Activity.Machines = []view.ActivityMachine{{MachineFresh: local, Buckets: []view.ActivityBucket{
		{TS: now.Add(-time.Minute), Events: 4, Sample: &model.MachineSample{MemUsed: 10}},
		{TS: now, Events: 2},
	}}}
	return s
}

func run(t *testing.T, d Dashboard, vars map[string]string) map[string]Result {
	t.Helper()
	f, err := Run(snapshot(), RunRequest{Dashboard: d, Vars: vars})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]Result{}
	for _, r := range f.Results {
		out[r.ID] = r
	}
	return out
}

func TestHomeRuns(t *testing.T) {
	results := run(t, Home(), nil)
	if len(results) != len(Home().Panels) {
		t.Fatalf("results = %d", len(results))
	}
	if got := results["waiting"].Frame.Rows[0][0]; got != 2.0 {
		t.Fatalf("waiting = %v, want 2 (historical waits excluded)", got)
	}
	if got := len(results["running"].Frame.Rows); got != 3 {
		t.Fatalf("sessions = %d, want 3 including workers", got)
	}
	inbox := results["inbox"].Frame
	id := 0
	for i, c := range inbox.Columns {
		if c.Name == "id" {
			id = i
		}
	}
	var order []string
	for _, r := range inbox.Rows {
		order = append(order, r[id].(string))
	}
	if strings.Join(order, ",") != "long,short,done" {
		t.Fatalf("inbox order = %v, want longest wait first then completions", order)
	}
}

func TestVariablesScopeAndOptions(t *testing.T) {
	f, err := Run(snapshot(), RunRequest{Dashboard: Home(), Vars: map[string]string{"project": "site"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.Options["project"], ","); got != "core,site" {
		t.Fatalf("project options = %q", got)
	}
	for _, r := range f.Results {
		if r.ID == "waiting" && r.Frame.Rows[0][0] != 0.0 {
			t.Fatalf("site waits = %v", r.Frame.Rows[0][0])
		}
		if r.ID == "running" && len(r.Frame.Rows) != 1 {
			t.Fatalf("site sessions = %v", r.Frame.Rows)
		}
	}
}

func TestGroupedTimeseriesAndEmpty(t *testing.T) {
	d := Dashboard{Name: "t", Panels: []Panel{
		{ID: "by_agent", Title: "By agent", Viz: "bar", Source: "sessions", By: "agent", Measure: "sum:mem_bytes"},
		{ID: "events", Title: "Events", Viz: "timeseries", Source: "activity", Measure: "sum:events"},
		{ID: "none", Title: "None", Viz: "table", Source: "sessions", Where: []Filter{{Column: "label", Value: "dead"}}},
		{ID: "notes", Title: "Notes", Viz: "text", Content: "Hello"},
	}}
	r := run(t, d, nil)
	if rows := r["by_agent"].Frame.Rows; len(rows) != 2 || rows[0][0] != "claude" || rows[0][1] != 400.0 {
		t.Fatalf("by agent = %v", rows)
	}
	if rows := r["events"].Frame.Rows; len(rows) != 2 || rows[1][2] != 2.0 {
		t.Fatalf("events = %v", rows)
	}
	if r["none"].Status != StatusEmpty || r["notes"].Status != StatusOK || r["notes"].Frame != nil {
		t.Fatalf("none=%+v notes=%+v", r["none"], r["notes"])
	}
}

func TestValidateNamesEveryProblem(t *testing.T) {
	err := Validate(Dashboard{Name: "x", Panels: []Panel{
		{ID: "Bad", Title: "t", Viz: "pie", Source: "sessions"},
		{ID: "ok", Title: "t", Viz: "bar", Source: "sessions", Where: []Filter{{Column: "nope", Value: "$missing"}}, Measure: "sum:title"},
		{ID: "ts", Title: "t", Viz: "timeseries", Source: "inbox"},
	}})
	var problems Problems
	if !errors.As(err, &problems) {
		t.Fatalf("err = %v", err)
	}
	text := err.Error()
	for _, want := range []string{"panels[0].id", "panels[0].viz", "panels[1].where[0].column", "$missing", "panels[1].by", `"title" is not numeric`, "panels[2].source"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %s", want, text)
		}
	}
}

func TestBuiltinsValidateAndRun(t *testing.T) {
	for _, id := range BuiltinIDs {
		d, ok := Builtin(id)
		if !ok {
			t.Fatalf("%s missing", id)
		}
		if _, err := Run(snapshot(), RunRequest{Dashboard: d}); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
}

func TestTimelineCarriesWaits(t *testing.T) {
	s := snapshot()
	start := s.ObservedAt.Add(-time.Hour)
	end := s.ObservedAt.Add(-30 * time.Minute)
	s.BoardData.Nodes = []view.FlowNode{{ID: 1, MachineID: "here", Title: "fix tz", StartedAt: start, LastActivityAt: s.ObservedAt, Waits: []view.FlowWait{{StartedAt: start.Add(time.Minute), EndedAt: &end, Reason: "permission"}}}, {ID: 2, Title: "other", StartedAt: start, LastActivityAt: start}}
	f, err := Run(s, RunRequest{Dashboard: Dashboard{Name: "t", Panels: []Panel{{ID: "tl", Title: "T", Viz: "timeline", Source: "flow", Limit: 1}}}})
	if err != nil {
		t.Fatal(err)
	}
	frame := f.Results[0].Frame
	if len(frame.Rows) != 1 || frame.Rows[0][0] != 1.0 || len(frame.Spans) != 1 || frame.Spans[0].Reason != "permission" {
		t.Fatalf("timeline = %+v", frame)
	}
}

// Every source's rows match its columns, and a view that leaves identity
// columns out still carries them, hidden, for the app's actions.
func TestRowsMatchColumnsAndKeepIdentity(t *testing.T) {
	snap := snapshot()
	for name, src := range sources {
		for _, row := range src.build(snap) {
			if len(row) != len(src.columns) {
				t.Errorf("%s: row has %d cells for %d columns", name, len(row), len(src.columns))
			}
		}
	}
	got := run(t, Dashboard{Name: "x", Panels: []Panel{
		{ID: "s", Title: "S", Viz: "table", Source: "sessions", Columns: []string{"title"}},
		{ID: "c", Title: "C", Viz: "table", Source: "cleanup", Columns: []string{"path"}},
	}}, nil)
	for id, want := range map[string]string{"s": "here", "c": "here"} {
		f := got[id].Frame
		i := slices.IndexFunc(f.Columns, func(c Column) bool { return c.Name == "machine_id" })
		if i < 0 || !f.Columns[i].Hidden || f.Rows[0][i] != want {
			t.Errorf("%s: machine_id = %+v in %v", id, f.Columns, f.Rows)
		}
	}
}
