package collector

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestRecordedMachineWarnings(t *testing.T) {
	d, st, _, _ := setup(t)
	t.Setenv("PATH", t.TempDir())
	at := d.now()
	samples := []struct {
		level string
		want  int
	}{{"critical", 0}, {"normal", 0}, {"critical", 0}, {"critical", 1}, {"critical", 1}, {"warning", 1}, {"critical", 1}, {"critical", 2}}
	for i, s := range samples {
		now := at.Add(time.Duration(i) * time.Minute)
		err := st.Tx(context.Background(), func(tx *store.Tx) error {
			return memoryPressureInbox(tx, model.MachineSample{TS: now, PressureLevel: s.level}, now)
		})
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, item := range inbox(t, st) {
			if item.Kind == "memory_pressure" {
				count++
				if item.Jump == nil || item.Jump.Kind != "machine" {
					t.Fatal(item)
				}
			}
		}
		if count != s.want {
			t.Fatalf("sample %d count %d want %d", i, count, s.want)
		}
	}
	// Unavailable collection breaks consecutiveness, including across restart.
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		if err := tx.SetMeta("machine:critical_count", "1"); err != nil {
			return err
		}
		return tx.SetMeta("machine:pressure_active", "0")
	}); err != nil {
		t.Fatal(err)
	}
	d.Adapters = nil
	p := &fakePlatform{machineErr: errors.New("unavailable")}
	d.Plat = p
	pass(t, d, st, Plan{Machine: true})
	raw, _ := st.Meta("machine:critical_count")
	if raw != "0" {
		t.Fatalf("failed sample retained streak %q", raw)
	}
}
func TestMachineDiskThreshold(t *testing.T) {
	d, st, _, _ := setup(t)
	at := d.now()
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		if err := lowDiskInbox(tx, d.Cfg.Machine, model.MachineSample{DiskFree: 15_000_000_000, DiskTotal: 1_000_000_000_000}, at); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	items := inbox(t, st)
	if len(items) != 1 || items[0].Kind != "low_disk" || items[0].Jump == nil {
		t.Fatal(items)
	}
}

func TestCriticalSameReadingDoesNotCountTwice(t *testing.T) {
	d, st, _, _ := setup(t)
	at := d.now()
	sample := model.MachineSample{TS: at, PressureLevel: "critical"}
	for range 2 {
		if err := st.Tx(context.Background(), func(tx *store.Tx) error { return memoryPressureInbox(tx, sample, at) }); err != nil {
			t.Fatal(err)
		}
	}
	if items := inbox(t, st); len(items) != 0 {
		t.Fatal(items)
	}
	sample.TS = at.Add(time.Minute)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error { return memoryPressureInbox(tx, sample, sample.TS) }); err != nil {
		t.Fatal(err)
	}
	if items := inbox(t, st); len(items) != 1 {
		t.Fatal(items)
	}
}

func TestUpgradeExistingLowDisk(t *testing.T) {
	d, st, _, _ := setup(t)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		if err := tx.SetMeta("cleanup:low_disk", "1"); err != nil {
			return err
		}
		if err := tx.UpsertInboxItem(model.InboxItem{ID: "low_disk:1", Kind: "low_disk", Summary: "old message", TS: d.now()}); err != nil {
			return err
		}
		return lowDiskInbox(tx, d.Cfg.Machine, model.MachineSample{DiskTotal: 1000, DiskFree: 1}, d.now())
	}); err != nil {
		t.Fatal(err)
	}
	items := inbox(t, st)
	if len(items) != 1 || items[0].ID != "low_disk:1" || items[0].Jump == nil || items[0].Summary == "old message" {
		t.Fatal(items)
	}
}

func TestMachineViewReusesCollectorProcessSample(t *testing.T) {
	d, st, p, a := setup(t)
	at := d.now()
	project := a.obs[0].Cwd
	p.procs[0].Exe = "/bin/agent"
	p.procs = append(p.procs, model.Proc{PID: 102, PPID: 101, StartTime: at.Add(-time.Minute), Exe: "/bin/worker", RSS: 100, Footprint: 200, MemoryMeasure: "physical_footprint", CPUSeconds: 1}, model.Proc{PID: 103, StartTime: at.Add(-2 * time.Hour), Exe: "/bin/old-server", RSS: 300})
	p.cwds = map[int32]string{101: project, 102: project, 103: project}
	pass(t, d, st, Plan{Procs: true, Machine: true, Cwds: true})
	now := at.Add(5 * time.Second)
	d.Now = func() time.Time { return now }
	p.procs[1].CPUSeconds = 3
	pass(t, d, st, Plan{Procs: true, Machine: true, Cwds: true})
	rows, err := st.Procs()
	if err != nil {
		t.Fatal(err)
	}
	if p.procCalls != 2 || p.cwdCalls != 1 {
		t.Fatalf("extra scan/cwd refresh: %d %d", p.procCalls, p.cwdCalls)
	}
	seen := false
	for _, proc := range rows {
		if proc.PID == 102 {
			seen = true
			if proc.Footprint != 200 || proc.MemoryMeasure != "physical_footprint" || proc.CPUPercent != 40 {
				t.Fatal(proc)
			}
		}
		if proc.PID == 103 && proc.Cwd != project {
			t.Fatal("non-agent cwd missing", proc)
		}
	}
	if !seen {
		t.Fatal("worker missing")
	}
}
