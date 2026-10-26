package panel

import (
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
	"testing"
)

func TestMachinePanelSources(t *testing.T) {
	s := storySnapshot()
	cpu := 42.0
	sample := model.MachineSample{CPUPercent: &cpu, DiskTotal: 500, DiskFree: 100}
	s.Summary.Sample = sample
	s.Activity.Machines[0].Buckets[0].Sample = &sample
	s.Machines.Rows = append(s.Machines.Rows, view.MachineRow{Sample: sample})
	for _, source := range []string{"activity", "summary", "machines"} {
		f, err := Run(s, RunRequest{Dashboard: Dashboard{Name: "Resources", Panels: []Panel{{ID: "r", Title: "Resources", Source: source, Viz: "table", Columns: []string{"disk_total_bytes", "cpu_percent"}, Limit: 200}}}})
		if err != nil {
			t.Fatal(source, err)
		}
		found := false
		for _, row := range f.Results[0].Frame.Rows {
			if row[0] == float64(500) && row[1] == cpu {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s lost CPU or disk total: %+v", source, f.Results[0].Frame)
		}
	}
}

func TestSessionPanelSource(t *testing.T) {
	f, err := Run(storySnapshot(), RunRequest{Dashboard: Dashboard{Name: "Fleet", Panels: []Panel{{ID: "s", Title: "Sessions", Source: "sessions", Viz: "table", Columns: []string{"id", "turns", "files", "edited"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range f.Results[0].Frame.Rows {
		if row[0] == float64(11) {
			if row[1] != "ok,waited,failed" || row[2] != float64(1) || row[3] != `["src/upload.ts"]` {
				t.Fatal(row)
			}
			return
		}
	}
	t.Fatal("missing session")
}

func TestActivityHeatmapPanelSource(t *testing.T) {
	f, err := Run(storySnapshot(), RunRequest{Dashboard: Dashboard{Name: "Activity", Panels: []Panel{{ID: "h", Title: "Tool calls", Source: "activity_heatmap", Viz: "timeseries", By: "project", Measure: "sum:tool_calls"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Results[0].Frame.Rows) != 72 {
		t.Fatalf("24 hours/project: %+v", f.Results[0])
	}
	if err := Validate(Dashboard{Name: "Bad", Panels: []Panel{{ID: "h", Title: "Bad", Source: "activity_heatmap", Viz: "bar", By: "project", Measure: "sum:file_contents"}}}); err == nil {
		t.Fatal("unknown measurement passed validation")
	}
}

func TestFragmentCarriesServiceHistory(t *testing.T) {
	f, err := Run(storySnapshot(), RunRequest{Dashboard: Header()})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.History.Attention) != 60 || len(f.History.Polls) != 30 {
		t.Fatalf("history omitted: %+v", f.History)
	}
}
