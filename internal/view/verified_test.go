package view

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestOrphansReportFootprintAndCPUAlongsideRSS(t *testing.T) {
	st := fixture(t, func(tx *store.Tx) error {
		if err := tx.ReplaceProcs([]model.Proc{
			{PID: 10, PPID: 1, StartTime: testNow, RSS: 12 << 20, Footprint: 3 << 30, MemoryMeasure: "physical_footprint", CPUPercent: 0.25},
			{PID: 11, PPID: 10, StartTime: testNow, RSS: 4 << 20, Footprint: 512 << 20, MemoryMeasure: "physical_footprint", CPUPercent: 0.5},
		}); err != nil {
			return err
		}
		// The candidate may precede the newest process sample. Display memory
		// and RSS from the same matched sample, not an older candidate total.
		return tx.ReplaceOrphans([]model.Orphan{{ID: "fixture", RootPID: 10, StartTime: testNow, PIDs: []int32{10, 11}, RSSTotal: 1 << 20, Verified: true}})
	})
	v, err := Orphans(st, Filter{}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct{ Rows []map[string]any }
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	row := decoded.Rows[0]
	if row["memory_bytes"] != float64(3758096384) || row["rss_total_bytes"] != float64(16777216) || row["memory_measure"] != "physical_footprint" || row["cpu_percent"] != 0.75 || row["resources_known"] != true {
		t.Fatalf("orphan resource accounting: %s", data)
	}
}

func TestOrphanResourcesUseRecordedWindowsTopology(t *testing.T) {
	st := fixture(t, func(tx *store.Tx) error {
		if err := tx.UpsertMachine(model.Machine{OS: "windows"}); err != nil {
			return err
		}
		if err := tx.ReplaceProcs([]model.Proc{
			{PID: 10, PPID: 1, StartTime: testNow, Footprint: 100, MemoryMeasure: "private_bytes"},
			// Windows retains the exited creator's PID; this older process is
			// unrelated to the new root which reused PID 10.
			{PID: 11, PPID: 10, StartTime: testNow.Add(-time.Minute), Footprint: 1000, MemoryMeasure: "private_bytes"},
		}); err != nil {
			return err
		}
		return tx.ReplaceOrphans([]model.Orphan{{ID: "fixture", RootPID: 10, StartTime: testNow, PIDs: []int32{10}}})
	})
	v, err := Orphans(st, Filter{}, testNow)
	if err != nil || len(v.Rows) != 1 || !v.Rows[0].ResourcesKnown || v.Rows[0].Memory != 100 {
		t.Fatalf("view=%+v err=%v", v, err)
	}
}

func TestOrphansRenderVerifiedAndSnapshotFooter(t *testing.T) {
	st := fixture(t, func(tx *store.Tx) error {
		return tx.ReplaceOrphans([]model.Orphan{{ID: "2:1", RootPID: 2, Verified: true}})
	})
	v, err := Orphans(st, Filter{}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Render(&out, v, RenderOpts{NoColor: true, Envelope: Envelope{Mode: "snapshot"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "VERIFIED") || !strings.Contains(out.String(), "unverified (snapshot)") {
		t.Fatal(out.String())
	}
	out.Reset()
	if err := Render(&out, v, RenderOpts{JSON: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"verified":true`) {
		t.Fatal(out.String())
	}
}
