package panel

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
)

func TestHeaderAllowsSixteenStatsAndGauges(t *testing.T) {
	d := Dashboard{Name: "Header"}
	for i := range 16 {
		viz := "stat"
		if i%2 == 1 {
			viz = "gauge"
		}
		d.Panels = append(d.Panels, Panel{ID: fmt.Sprintf("metric_%d", i), Title: "Metric", Viz: viz, Source: "summary", Measure: "sum:agents"})
	}
	if err := Validate(d); err != nil {
		t.Fatal(err)
	}
	if err := ValidateHeader(d); err != nil {
		t.Fatalf("sixteen stats and gauges: %v", err)
	}
	d.Panels = append(d.Panels, Panel{ID: "extra", Title: "Extra", Viz: "stat", Source: "summary"})
	if err := ValidateHeader(d); err == nil || !strings.Contains(err.Error(), "the header shows at most 16 metrics") {
		t.Fatalf("seventeen panels: %v", err)
	}
	d.Panels = []Panel{{ID: "bar", Title: "Bar", Viz: "bar", Source: "sessions", By: "agent"}}
	if err := ValidateHeader(d); err == nil || !strings.Contains(err.Error(), "the header shows only stat and gauge panels") {
		t.Fatalf("bar in header: %v", err)
	}
}

func TestDiskUsedBytes(t *testing.T) {
	for _, tc := range []struct {
		name        string
		total, free uint64
		want        any
	}{
		{"known", 994e9, 234e9, float64(760e9)},
		{"unknown total", 0, 234e9, nil},
		{"full disk", 100e9, 0, float64(100e9)},
		{"invalid free", 100e9, 101e9, nil},
	} {
		for _, source := range []string{"activity", "local_activity", "summary", "machines"} {
			t.Run(tc.name+"/"+source, func(t *testing.T) {
				s := snapshot()
				sample := model.MachineSample{DiskTotal: tc.total, DiskFree: tc.free}
				s.Summary.MachineID, s.Summary.Sample = "here", sample
				s.Activity.Machines = []view.ActivityMachine{{MachineID: "here", Buckets: []view.ActivityBucket{{TS: s.ObservedAt, Sample: &sample}}}}
				s.Machines.Rows = []view.MachineRow{{Machine: model.Machine{MachineID: "here"}, Sample: &sample}}
				d := Dashboard{Name: "Disk", Panels: []Panel{{ID: "disk", Title: "Disk used", Viz: "stat", Source: source, Measure: "max:disk_used_bytes"}}}
				f, err := Run(s, RunRequest{Dashboard: d})
				if err != nil {
					t.Fatal(err)
				}
				r := f.Results[0]
				if got := r.Frame.Rows[0][0]; got != tc.want || r.Frame.Columns[0].Unit != "bytes" {
					t.Fatalf("disk used = %v (%s), want %v bytes", got, r.Frame.Columns[0].Unit, tc.want)
				}
			})
		}
	}
}

func TestSummaryMachinesReachable(t *testing.T) {
	s := snapshot()
	s.Machines.Rows = []view.MachineRow{
		{Machine: model.Machine{MachineID: "here"}},
		{Machine: model.Machine{MachineID: "live", State: "live", LastContactAt: s.ObservedAt.Add(-time.Second)}},
		{Machine: model.Machine{MachineID: "old", State: "live", LastContactAt: s.ObservedAt.Add(-time.Minute)}},
	}
	d := Dashboard{Name: "Machines", Panels: []Panel{{ID: "up", Title: "Machines up", Viz: "stat", Source: "summary", Measure: "sum:machines_reachable", Total: "sum:machines"}}}
	f, err := Run(s, RunRequest{Dashboard: d})
	if err != nil {
		t.Fatal(err)
	}
	if r := f.Results[0]; r.Frame.Rows[0][0] != float64(2) || r.Total != float64(3) {
		t.Fatalf("machines up = %+v, want 2 of 3", r)
	}
}

func TestStatCaption(t *testing.T) {
	s := snapshot()
	s.Orphans.Rows = make([]view.OrphanRow, 2)
	p := Panel{ID: "leftover", Title: "Leftover", Viz: "stat", Source: "summary", Measure: "sum:leftover_bytes", Caption: "sum:leftover"}
	f, err := Run(s, RunRequest{Dashboard: Dashboard{Name: "Caption", Panels: []Panel{p}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Results[0].Caption; got != float64(2) {
		t.Fatalf("caption = %v, want 2", got)
	}
	for _, viz := range []string{"table", "gauge", "text"} {
		p.Viz, p.Content = viz, "Text"
		if viz == "text" {
			p.Source = ""
		}
		if err := Validate(Dashboard{Name: "Caption", Panels: []Panel{p}}); err == nil || !strings.Contains(err.Error(), "only stat panels show a caption") {
			t.Errorf("caption on %s: %v", viz, err)
		}
	}
	p.Viz, p.Source = "stat", "summary"
	for _, caption := range []string{"leftover", "sum:missing", "sum:machine"} {
		p.Caption = caption
		if caption == "sum:machine" {
			p.Source, p.Measure = "machines", "count"
		}
		if err := Validate(Dashboard{Name: "Caption", Panels: []Panel{p}}); err == nil || !strings.Contains(err.Error(), "panels[0].caption") {
			t.Errorf("invalid caption %s: %v", caption, err)
		}
	}
}
