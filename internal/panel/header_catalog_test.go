package panel

import (
	"reflect"
	"testing"

	"github.com/labstack/driga/internal/cleanup"
)

func TestCatalogValid(t *testing.T) {
	catalog := HeaderCatalog()
	if len(catalog) != 18 {
		t.Fatalf("catalog has %d widgets, want 18", len(catalog))
	}
	ids := map[string]bool{}
	for _, p := range catalog {
		t.Run(p.ID, func(t *testing.T) {
			if ids[p.ID] {
				t.Fatalf("duplicate catalog id %q", p.ID)
			}
			ids[p.ID] = true
			d := Dashboard{Name: "Widget", Panels: []Panel{p}}
			if err := Validate(d); err != nil {
				t.Fatal(err)
			}
			if err := ValidateHeader(d); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHeaderDefaultMatchesCatalog(t *testing.T) {
	want := []string{"waiting", "finished", "stuck", "leftover", "reclaim", "memory"}
	if got := HeaderDefault(); !reflect.DeepEqual(got, want) {
		t.Fatalf("default = %v, want %v", got, want)
	}
	catalog := map[string]Panel{}
	for _, p := range HeaderCatalog() {
		catalog[p.ID] = p
	}
	var ids []string
	for _, p := range Header().Panels {
		ids = append(ids, p.ID)
		if !reflect.DeepEqual(p, catalog[p.ID]) {
			t.Errorf("default panel differs from catalog: %+v", p)
		}
	}
	if !reflect.DeepEqual(ids, HeaderDefault()) {
		t.Fatalf("header ids = %v, want %v", ids, HeaderDefault())
	}
}

func TestCatalogRunsOnSnapshot(t *testing.T) {
	f, err := Run(storySnapshot(), RunRequest{Dashboard: Dashboard{Name: "Catalog", Panels: HeaderCatalog()}})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Results) != 18 {
		t.Fatalf("got %d results, want 18", len(f.Results))
	}
	for _, r := range f.Results {
		if r.Status != StatusOK && r.Status != StatusEmpty {
			t.Errorf("%s: %s (%s)", r.ID, r.Status, r.Message)
		}
	}
}

func TestCatalogMatchesSpec(t *testing.T) {
	want := []struct{ id, title, viz, source, measure, total, caption string }{
		{"waiting", "Need you", "stat", "summary", "sum:need_you", "", ""},
		{"finished", "To review", "stat", "summary", "sum:to_review", "", ""},
		{"stuck", "Stuck", "stat", "summary", "sum:attention_stuck", "", ""},
		{"leftover", "Leftover", "stat", "summary", "sum:leftover_bytes", "", "sum:leftover"},
		{"reclaim", "Reclaimable", "stat", "cleanup", "sum:size_bytes", "", ""},
		{"memory", "Memory", "stat", "local_activity", "max:mem_used_bytes", "max:mem_total_bytes", ""},
		{"cpu", "CPU", "gauge", "local_activity", "max:cpu_percent", "", ""},
		{"disk", "Disk used", "stat", "local_activity", "max:disk_used_bytes", "max:disk_total_bytes", ""},
		{"load", "Load", "stat", "local_activity", "max:load1", "", ""},
		{"swap", "Swap", "stat", "local_activity", "max:swap_used_bytes", "", ""},
		{"events", "Events/min", "stat", "local_activity", "max:events", "", ""},
		{"agents", "Agents", "stat", "summary", "sum:agents", "", ""},
		{"working", "Working", "stat", "summary", "sum:working", "", ""},
		{"waiting_now", "Waiting", "stat", "summary", "sum:waiting", "", ""},
		{"inbox", "Inbox", "stat", "summary", "sum:inbox", "", ""},
		{"orphans", "Leftover trees", "stat", "summary", "sum:orphans", "", ""},
		{"machines_up", "Machines up", "stat", "summary", "sum:machines_reachable", "sum:machines", ""},
		{"agent_memory", "Agent memory", "stat", "costs", "sum:mem_bytes", "", ""},
	}
	catalog := HeaderCatalog()
	if len(catalog) != len(want) {
		t.Fatalf("catalog has %d widgets, want %d", len(catalog), len(want))
	}
	for i, w := range want {
		p := catalog[i]
		if p.ID != w.id || p.Title != w.title || p.Viz != w.viz || p.Source != w.source || p.Measure != w.measure || p.Total != w.total || p.Caption != w.caption {
			t.Errorf("catalog[%d] = %+v, want %+v", i, p, w)
		}
		var where []Filter
		if p.ID == "reclaim" {
			where = []Filter{{Column: "status", Value: "ready"}}
		}
		if !reflect.DeepEqual(p.Where, where) {
			t.Errorf("%s filters = %v, want %v", p.ID, p.Where, where)
		}
		if p.ID == "cpu" {
			if p.Max == nil || *p.Max != 100 {
				t.Errorf("CPU max = %v, want 100", p.Max)
			}
		} else if p.Max != nil {
			t.Errorf("%s has unexpected max %v", p.ID, p.Max)
		}
	}
}

func TestReclaimCountsOnlyReadyDiskCleanup(t *testing.T) {
	s := storySnapshot()
	s.Summary.Reclaim = 999999
	s.Cleanup = []cleanup.Candidate{
		{Status: "ready", SizeKnown: true, Size: 2048}, {Status: "ready", SizeKnown: true, Size: 4096},
		{Status: "needs_review", SizeKnown: true, Size: 8192}, {Status: "report_only", SizeKnown: true, Size: 16384},
		{Status: "refused", SizeKnown: true, Size: 32768}, {Status: "unknown", SizeKnown: true, Size: 65536},
	}
	for _, candidates := range [][]cleanup.Candidate{s.Cleanup, s.Cleanup[2:], nil} {
		s.Cleanup = candidates
		want := float64(0)
		if len(candidates) == 6 {
			want = 6144
		}
		f, err := Run(s, RunRequest{Dashboard: Header()})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range f.Results {
			if r.ID == "reclaim" {
				if r.Status != StatusOK || r.Frame.Rows[0][0] != want || r.Frame.Columns[0].Unit != "bytes" {
					t.Fatalf("reclaim = %+v, want %v bytes", r, want)
				}
			}
		}
	}
}

func TestReclaimUnknownAndCapped(t *testing.T) {
	s := storySnapshot()
	s.Cleanup = []cleanup.Candidate{{Status: "ready", SizeKnown: true, Size: 7, SizeCapped: true}, {Status: "ready"}}
	read := func() Result {
		f, err := Run(s, RunRequest{Dashboard: Header()})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range f.Results {
			if r.ID == "reclaim" {
				return r
			}
		}
		t.Fatal("missing reclaim")
		return Result{}
	}
	if r := read(); r.Frame.Rows[0][0] != nil {
		t.Fatalf("partial total should be unknown: %+v", r)
	}
	s.Cleanup[1].SizeKnown = true
	if r := read(); r.Frame.Rows[0][0] != float64(7) || r.Title != "Reclaimable (at least)" {
		t.Fatalf("lost lower bound: %+v", r)
	}
}
