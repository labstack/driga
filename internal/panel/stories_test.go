package panel

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
	"github.com/labstack/driga/internal/viewer"
)

// storiesPath holds the fragments the UI's Storybook renders: Driga's own
// dashboards run by this package over a fictional fleet, so stories show
// exactly what the service sends. DRIGA_WRITE_STORIES=1 regenerates it.
const storiesPath = "../../ui/src/fixtures/fragments.json"

var studioTotal float64 = 32 << 30

var storyNow = time.Date(2026, 3, 14, 15, 0, 0, 0, time.UTC)

// storySnapshot is a fictional fleet of two machines with a session in every
// state the app shows: waiting on a person, working, inferred, idle,
// finished, suspected stuck and dead, with completions to review, a machine
// warning, leftover processes and disk to reclaim. No name, path or address
// belongs to a real person or project.
func storySnapshot() viewer.Snapshot {
	now := storyNow
	s := viewer.Snapshot{LocalMachineID: "studio-id"}
	s.ObservedAt = now
	studio := view.MachineFresh{MachineName: "studio"}
	build := view.MachineFresh{MachineName: "build-01"}
	at := func(d time.Duration) time.Time { return now.Add(-d) }
	gb := func(n float64) uint64 { return uint64(n * (1 << 30)) }
	mb := func(n float64) uint64 { return uint64(n * (1 << 20)) }
	s.Agents.Rows = []view.AgentRow{
		{ID: 11, MachineID: "studio-id", MachineFresh: studio, Title: "Add rate limiting to the upload API", Label: "waiting", Adapter: "claude", Project: "atlas", Branch: "feat/upload-limits", Age: 42 * time.Minute, Mem: mb(612), LastActivityAt: at(3 * time.Minute)},
		{ID: 12, MachineID: "studio-id", MachineFresh: studio, Title: "Fix the flaky checkout test", Label: "working", Adapter: "claude", Project: "harbor", Branch: "fix/checkout-flake", Age: 18 * time.Minute, Mem: gb(1.4), LastActivityAt: at(10 * time.Second),
			Children: []view.AgentRow{{ID: 13, MachineID: "studio-id", MachineFresh: studio, Title: "Run the payment suite", Label: "working", Adapter: "claude", Project: "harbor", Branch: "fix/checkout-flake", Age: 6 * time.Minute, Mem: mb(380), LastActivityAt: at(20 * time.Second)}}},
		{ID: 14, MachineID: "studio-id", MachineFresh: studio, Title: "", Label: "~working", Heuristic: true, Evidence: "files changed 1m ago; no hook events", Adapter: "codex", Project: "beacon", Branch: "chore/deps", Age: 2 * time.Hour, LastActivityAt: at(time.Minute)},
		{ID: 15, MachineID: "studio-id", MachineFresh: studio, Title: "Write the release notes", Label: "idle", Adapter: "claude", Project: "atlas", Branch: "main", Age: 3 * time.Hour, Mem: mb(240), LastActivityAt: at(48 * time.Minute)},
		{ID: 16, MachineID: "build-id", MachineFresh: build, Title: "Migrate billing to the v2 events", Label: "~stalled?", Heuristic: true, Evidence: "busy, no output for 34m, CPU idle", Adapter: "claude", Project: "harbor", Branch: "feat/billing-v2", Age: 5 * time.Hour, Mem: gb(2.1), LastActivityAt: at(34 * time.Minute)},
		{ID: 17, MachineID: "build-id", MachineFresh: build, Title: "Profile the search index", Label: "dead", Evidence: "process gone with a turn open", Adapter: "claude", Project: "beacon", Branch: "perf/index", Age: 7 * time.Hour, LastActivityAt: at(2 * time.Hour)},
		{ID: 18, MachineID: "studio-id", MachineFresh: studio, Title: "Tidy the docs site", Label: "finished", Adapter: "gemini", Project: "beacon", Branch: "docs/tidy", Age: 26 * time.Hour, Mem: mb(90), LastActivityAt: at(20 * time.Hour)},
		{ID: 19, MachineID: "studio-id", MachineFresh: studio, Title: "", Label: "unknown", Adapter: "agy", Project: "sandbox", Age: 4 * time.Hour, Mem: mb(130)},
	}
	ended := at(5 * time.Minute)
	s.Inbox.Rows = []view.InboxRow{
		{MachineID: "studio-id", MachineFresh: studio, InboxItem: model.InboxItem{ID: "wait-11", Kind: "waiting", SessionID: 11, Reason: "permission", Tool: "Bash", Detail: "npm run migrate -- --env staging", TS: at(4 * time.Minute)}, Project: "atlas", Adapter: "claude", Title: "Add rate limiting to the upload API", Branch: "feat/upload-limits", Waited: 4 * time.Minute, Age: 4 * time.Minute},
		{MachineID: "build-id", MachineFresh: build, InboxItem: model.InboxItem{ID: "wait-16", Kind: "waiting", SessionID: 16, Reason: "question", Detail: "Keep the old webhook endpoint during the cutover?", TS: at(22 * time.Minute)}, Project: "harbor", Adapter: "claude", Title: "Migrate billing to the v2 events", Branch: "feat/billing-v2", Waited: 22 * time.Minute, Age: 22 * time.Minute},
		{MachineID: "studio-id", MachineFresh: studio, InboxItem: model.InboxItem{ID: "pressure-3", Kind: "memory_pressure", TS: at(9 * time.Minute), Summary: "Memory pressure on studio: 29.1 of 32 GB used, swap 6.2 GB. Largest: harbor sessions, 3.5 GB."}, Age: 9 * time.Minute},
		{MachineID: "build-id", MachineFresh: build, InboxItem: model.InboxItem{ID: "disk-2", Kind: "low_disk", TS: at(3 * time.Hour), EndedAt: &ended, Summary: "Low disk on build-01: 8 GB free."}, Age: 3 * time.Hour},
		{MachineID: "studio-id", MachineFresh: studio, InboxItem: model.InboxItem{ID: "done-12a", Kind: "finished", SessionID: 12, TS: at(12 * time.Minute), Summary: "Retried the checkout test 40 times; it passes. The race was in the cart fixture."}, Project: "harbor", Adapter: "claude", Title: "Fix the flaky checkout test", Branch: "fix/checkout-flake", Age: 12 * time.Minute,
			Checkout: &model.Worktree{Path: "/home/dev/src/harbor", Dirty: 2, Untracked: 1, Ahead: 1, LastCommitSubject: "test: seed the cart fixture"}, Files: []string{"/home/dev/src/harbor/test/checkout.test.ts", "/home/dev/src/harbor/test/fixtures/cart.ts"}},
		{MachineID: "studio-id", MachineFresh: studio, InboxItem: model.InboxItem{ID: "done-12b", Kind: "finished", SessionID: 12, TS: at(30 * time.Minute)}, Project: "harbor", Adapter: "claude", Title: "Fix the flaky checkout test", Branch: "fix/checkout-flake", Age: 30 * time.Minute},
		{MachineID: "studio-id", MachineFresh: studio, InboxItem: model.InboxItem{ID: "done-15", Kind: "finished", SessionID: 15, TS: at(50 * time.Minute), Summary: "turn finished"}, Project: "atlas", Adapter: "claude", Title: "Write the release notes", Branch: "main", Age: 50 * time.Minute,
			Checkout: &model.Worktree{Path: "/home/dev/src/atlas", LastCommitSubject: "docs: release notes for 2.4"}},
		{MachineID: "studio-id", MachineFresh: studio, InboxItem: model.InboxItem{ID: "done-18", Kind: "finished", SessionID: 18, TS: at(20 * time.Hour), Summary: "turn finished"}, Project: "beacon", Adapter: "gemini", Title: "Tidy the docs site", Branch: "docs/tidy", Age: 20 * time.Hour},
	}
	s.Orphans.Rows = []view.OrphanRow{
		{MachineID: "studio-id", MachineFresh: studio, Orphan: model.Orphan{ID: "4242:1", RootPID: 4242, Signature: "vite dev server", ProcCount: 3, RSSTotal: mb(820), Cwd: "/home/dev/src/atlas", Verified: true}, Age: 2 * time.Hour},
		{MachineID: "studio-id", MachineFresh: studio, Orphan: model.Orphan{ID: "5150:1", RootPID: 5150, Signature: "jest --watch", ProcCount: 6, RSSTotal: gb(1.2), Cwd: "/home/dev/src/harbor"}, Age: 40 * time.Minute},
	}
	s.Cleanup = []cleanup.Candidate{
		{ID: "wt-1", Kind: "worktree", Path: "/home/dev/src/harbor-wt/fix-cart", Repo: "harbor", Branch: "fix/cart", Status: "ready", Size: int64(gb(1.8))},
		{ID: "wt-2", Kind: "worktree", Path: "/home/dev/src/atlas-wt/spike", Repo: "atlas", Branch: "spike/cache", Status: "dirty", Size: int64(mb(640))},
		{ID: "tmp-1", Kind: "scratch", Path: "/tmp/agent-scratch-7", Status: "ready", Size: int64(gb(3.2))},
	}
	s.Summary = view.SummaryView{Agents: 9, Working: 3, Waiting: 2, Stalled: 1, Inbox: 8, Orphans: 2, Reclaim: gb(5.0)}
	s.Machine.SessionCosts = []view.SessionCost{
		{SessionID: 12, Adapter: "claude", Project: "harbor", Memory: gb(1.4), CPUPercent: 88},
		{SessionID: 13, Adapter: "claude", Project: "harbor", Memory: mb(380), CPUPercent: 42},
		{SessionID: 11, Adapter: "claude", Project: "atlas", Memory: mb(612), CPUPercent: 3},
		{SessionID: 15, Adapter: "claude", Project: "atlas", Memory: mb(240), CPUPercent: 0},
		{SessionID: 19, Adapter: "agy", Project: "sandbox", Memory: mb(130), CPUPercent: 1},
		{SessionID: 18, Adapter: "gemini", Project: "beacon", Memory: mb(90), CPUPercent: 0},
	}
	for i, m := range []struct {
		fresh view.MachineFresh
		id    string
		base  float64
		total float64
	}{{studio, "studio-id", 24, 32}, {build, "build-id", 11, 16}} {
		am := view.ActivityMachine{MachineFresh: m.fresh, MachineID: m.id}
		for n := 59; n >= 0; n-- {
			// A deterministic wave, so the chart has shape and the fixture
			// does not change between runs.
			used := m.base + float64((n*7+i*3)%11)/4
			am.Buckets = append(am.Buckets, view.ActivityBucket{TS: now.Add(-time.Duration(n) * time.Minute), Events: (n*5 + i) % 9,
				Sample: &model.MachineSample{MemUsed: gb(used), MemTotal: gb(m.total), SwapUsed: gb(float64(n%5) + 2), DiskFree: gb(120 - float64(i)*100), Load1: 2 + float64(n%4)}})
		}
		s.Activity.Machines = append(s.Activity.Machines, am)
	}
	mem := func(n uint64) *uint64 { return &n }
	waitEnd := at(30 * time.Minute)
	s.BoardData.Nodes = []view.FlowNode{
		{ID: 11, MachineID: "studio-id", Title: "Add rate limiting to the upload API", Label: "waiting", Adapter: "claude", Project: "atlas", Branch: "feat/upload-limits", StartedAt: at(42 * time.Minute), LastActivityAt: at(3 * time.Minute), Mem: mem(mb(612)), LastTool: "Bash",
			Waits: []view.FlowWait{{ID: "w1", StartedAt: at(4 * time.Minute), Reason: "permission"}, {ID: "w0", StartedAt: at(35 * time.Minute), EndedAt: &waitEnd, Reason: "question"}}},
		{ID: 12, MachineID: "studio-id", Title: "Fix the flaky checkout test", Label: "working", Adapter: "claude", Project: "harbor", Branch: "fix/checkout-flake", StartedAt: at(18 * time.Minute), LastActivityAt: at(10 * time.Second), Mem: mem(gb(1.4)), LastTool: "Edit"},
		{ID: 16, MachineID: "build-id", Title: "Migrate billing to the v2 events", Label: "~stalled?", Adapter: "claude", Project: "harbor", Branch: "feat/billing-v2", StartedAt: at(55 * time.Minute), LastActivityAt: at(34 * time.Minute), Mem: mem(gb(2.1)),
			Waits: []view.FlowWait{{ID: "w2", StartedAt: at(22 * time.Minute), Reason: "question"}}},
	}
	s.BoardData.History = []view.HistoryRow{
		{MachineID: "studio-id", ID: 21, Title: "Upgrade the image pipeline", Project: "atlas", Branch: "feat/images", Adapter: "claude", StartedAt: at(30 * time.Hour), EndedAt: at(26 * time.Hour), Resume: "cd '/home/dev/src/atlas' && claude --resume 'a1b2c3'"},
		{MachineID: "build-id", ID: 22, Title: "Split the payments service", Project: "harbor", Branch: "refactor/payments", Adapter: "codex", StartedAt: at(4 * 24 * time.Hour), EndedAt: at(3 * 24 * time.Hour), Resume: "cd '/home/dev/src/harbor' && codex resume 'd4e5f6'"},
		{MachineID: "studio-id", ID: 23, Title: "Explore the plugin API", Project: "sandbox", Adapter: "agy", StartedAt: at(9 * 24 * time.Hour), EndedAt: at(9*24*time.Hour - time.Hour)},
	}
	return s
}

// storyDashboards are the fragments the stories need, by name.
func storyDashboards() map[string]RunRequest {
	all := Dashboard{Name: "Every panel", Description: "One panel of each kind, over the fictional fleet.", Panels: []Panel{
		{ID: "stat", Title: "Live sessions", Viz: "stat", Source: "sessions"},
		{ID: "stat_total", Title: "Memory used", Viz: "stat", Source: "activity", Measure: "max:mem_used_bytes", Total: "max:mem_total_bytes"},
		{ID: "gauge", Title: "Studio memory", Viz: "gauge", Source: "activity", Measure: "max:mem_used_bytes", Where: []Filter{{Column: "machine", Value: "studio"}}, Max: &studioTotal},
		{ID: "timeseries", Title: "Machine memory used", Viz: "timeseries", Source: "activity", By: "machine", Measure: "max:mem_used_bytes"},
		{ID: "timeseries_one", Title: "Swap used on studio", Viz: "timeseries", Source: "activity", Measure: "max:swap_used_bytes", Where: []Filter{{Column: "machine", Value: "studio"}}},
		{ID: "bar", Title: "Memory by session", Viz: "bar", Source: "costs", By: "session_id", Measure: "sum:mem_bytes"},
		{ID: "bar_project", Title: "Memory by project", Viz: "bar", Source: "costs", By: "project", Measure: "sum:mem_bytes"},
		{ID: "table", Title: "Sessions", Viz: "table", Source: "sessions", Columns: []string{"state", "title", "project", "branch", "agent", "machine", "last_activity_at", "mem_bytes"}},
		{ID: "list", Title: "Leftover process trees", Viz: "list", Source: "orphans", Columns: []string{"signature", "processes", "rss_bytes", "verified"}, Sort: "-rss_bytes"},
		{ID: "inbox", Title: "Inbox", Viz: "inbox", Source: "inbox"},
		{ID: "cards", Title: "Disk to reclaim", Viz: "cards", Source: "cleanup", Columns: []string{"path", "kind", "status", "size_bytes"}},
		{ID: "timeline", Title: "Last hour", Viz: "timeline", Source: "flow"},
		{ID: "text", Title: "Notes", Viz: "text", Content: "Measured on **studio** at 15:00.\n\n| Project | Disk |\n|---|---|\n| atlas | 12 GB |\n| harbor | 8.4 GB |\n| beacon | 2.1 GB |"},
		{ID: "empty", Title: "Stuck sessions", Viz: "table", Source: "sessions", Where: []Filter{{Column: "state", Value: "nothing"}}, Empty: "No session looks stuck."},
		{ID: "error", Title: "Broken panel", Viz: "bar", Source: "costs", By: "project", Measure: "sum:no_such_column"},
	}}
	search := Dashboard{Name: "Search", Panels: []Panel{
		{ID: "live", Title: "Live", Viz: "table", Source: "sessions"},
		{ID: "ended", Title: "Ended", Viz: "table", Source: "history"},
	}}
	return map[string]RunRequest{
		"home":   {Dashboard: Home()},
		"header": {Dashboard: Header()},
		"inbox":  {Dashboard: Dashboard{Name: "Inbox", Panels: []Panel{{ID: "inbox", Title: "Inbox", Viz: "inbox", Source: "inbox"}}}},
		"search": {Dashboard: search},
		"all":    {Dashboard: all},
	}
}

func storyFragments(t *testing.T) []byte {
	t.Helper()
	out := map[string]Fragment{}
	for name, req := range storyDashboards() {
		snap := storySnapshot()
		f, err := runLenient(snap, req)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out[name] = f
	}
	// Home with nothing going on: every panel empty, the all-clear strip.
	quiet := storySnapshot()
	quiet.Agents.Rows, quiet.Inbox.Rows, quiet.Orphans.Rows, quiet.Cleanup, quiet.Machine.SessionCosts = nil, nil, nil, nil, nil
	quiet.Summary = view.SummaryView{}
	for _, name := range []string{"home", "header", "inbox"} {
		f, err := Run(quiet, storyDashboards()[name])
		if err != nil {
			t.Fatal(err)
		}
		out[name+"_quiet"] = f
	}
	b, err := json.MarshalIndent(out, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	return append(b, '\n')
}

// runLenient runs a dashboard without validation, so the fixture can hold a
// panel that fails, as one does when a saved spec meets a changed source.
func runLenient(snap viewer.Snapshot, req RunRequest) (Fragment, error) {
	if err := Validate(req.Dashboard); err == nil {
		return Run(snap, req)
	}
	broken := req.Dashboard
	var ok []Panel
	var bad []Panel
	for _, p := range broken.Panels {
		if Validate(Dashboard{Name: "x", Panels: []Panel{p}}) == nil {
			ok = append(ok, p)
		} else {
			bad = append(bad, p)
		}
	}
	f, err := Run(snap, RunRequest{Dashboard: Dashboard{Name: broken.Name, Description: broken.Description, Panels: ok}})
	if err != nil {
		return f, err
	}
	for _, p := range bad {
		msg := Validate(Dashboard{Name: "x", Panels: []Panel{p}}).Error()
		f.Dashboard.Panels = append(f.Dashboard.Panels, p)
		f.Results = append(f.Results, Result{ID: p.ID, Status: StatusError, Message: msg})
	}
	return f, nil
}

func TestStoryFragmentsAreCurrent(t *testing.T) {
	want := storyFragments(t)
	if os.Getenv("DRIGA_WRITE_STORIES") == "1" {
		if err := os.WriteFile(storiesPath, want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(storiesPath)
	if err != nil || string(got) != string(want) {
		t.Fatalf("%s is out of date; run DRIGA_WRITE_STORIES=1 go test ./internal/panel", storiesPath)
	}
}
