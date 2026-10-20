package view

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestMachineAppsGroupingAndProofs(t *testing.T) {
	start := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	ss := []model.Session{{ID: 251, SessionObs: model.SessionObs{PID: 20, ProcStart: start, StartedAt: start, Cwd: "/projects/fixture"}}}
	ps := []model.Proc{
		{PID: 10, Exe: "/Applications/Terminal.app/Contents/MacOS/Terminal"},
		{PID: 20, PPID: 10, Exe: "/bin/agent", StartTime: start, RSS: 10},
		{PID: 30, PPID: 20, Exe: "/bin/worker", StartTime: start.Add(time.Second), RSS: 20, CPUPercent: 70},
		{PID: 40, Exe: "/bin/worker", Cwd: "/projects/fixture/src", StartTime: start.Add(time.Second), RSS: 30},
		{PID: 41, Exe: "/bin/worker", Cwd: "/projects/fixture", StartTime: start.Add(-time.Second), RSS: 40},
		{PID: 42, Exe: "/bin/worker", Cwd: "/projects/fixture-other", StartTime: start.Add(time.Second), RSS: 50},
		{PID: 50, PPID: 10, Exe: "/Applications/Browser.app/Contents/Frameworks/Helper.app/Contents/MacOS/Helper", RSS: 60},
		{PID: 60, PPID: 61, Exe: "/bin/cycle"}, {PID: 61, PPID: 60, Exe: "/bin/cycle"},
	}
	mem, cpu := machineApps(ps, ss)
	if len(mem) != 4 || cpu[0].Name != "Terminal" || cpu[0].CPUPercent != 70 {
		t.Fatalf("memory=%+v cpu=%+v", mem, cpu)
	}
	got := map[int32]MachineProcess{}
	for _, a := range mem {
		for _, p := range a.Processes {
			got[p.PID] = p
		}
	}
	for _, pid := range []int32{20, 30, 40} {
		if got[pid].Attribution == nil || got[pid].Attribution.SessionID != 251 {
			t.Fatalf("missing proof for %d: %+v", pid, got[pid])
		}
	}
	for _, pid := range []int32{41, 42, 50, 60} {
		if got[pid].Attribution != nil {
			t.Fatalf("false proof for %d: %+v", pid, got[pid])
		}
	}
	if mem[1].Name != "Helper" {
		t.Fatalf("nearest nested bundle: %+v", mem)
	}
	// A reused session PID must never prove ancestry.
	ss[0].ProcStart = start.Add(-time.Hour)
	mem, _ = machineApps(ps, ss)
	for _, a := range mem {
		for _, p := range a.Processes {
			if p.PID == 30 && p.Attribution != nil {
				t.Fatal("reused PID attributed")
			}
		}
	}
}

func TestMachineTopTenAndMixedMeasures(t *testing.T) {
	ps := []model.Proc{}
	for i := range 12 {
		ps = append(ps, model.Proc{PID: int32(i + 1), Exe: fmt.Sprintf("/bin/app-%02d", i), RSS: uint64(i + 1), CPUPercent: float64(12 - i)})
	}
	mem, cpu := machineApps(ps, nil)
	if len(mem) != 10 || len(cpu) != 10 || mem[0].Name != "app-11" || cpu[0].Name != "app-00" {
		t.Fatalf("%+v %+v", mem, cpu)
	}
	ps = []model.Proc{{PID: 1, Exe: "/bin/app", RSS: 10, Footprint: 15, MemoryMeasure: "physical_footprint"}, {PID: 2, Exe: "/bin/app", RSS: 20, Swap: 30, MemoryMeasure: "rss_swap"}}
	mem, _ = machineApps(ps, nil)
	if mem[0].Memory != 65 || mem[0].MemoryMeasure != "mixed" {
		t.Fatal(mem)
	}
	b, err := json.Marshal(mem[0].Processes[0])
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	if v["memory_bytes"] != float64(50) || v["memory_measure"] != "rss_swap" {
		t.Fatalf("process marshal lost view fields: %s", b)
	}
}

func TestMachineAttributionUsesKnownCheckoutRoot(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	repoID := int64(1)
	ss := []model.Session{{ID: 1, RepoID: &repoID, SessionObs: model.SessionObs{PID: 10, ProcStart: at, StartedAt: at, Cwd: "/worktrees/branch/src"}}}
	ps := []model.Proc{{PID: 10, StartTime: at, Exe: "/bin/agent"}, {PID: 20, StartTime: at.Add(time.Second), Cwd: "/worktrees/branch/server", Exe: "/bin/server"}, {PID: 30, StartTime: at.Add(time.Second), Cwd: "/worktrees/other", Exe: "/bin/server"}}
	ss = machineProjectSessions(ss, []model.Repo{{ID: 1, Path: "/projects/main"}}, []model.Worktree{{RepoID: 1, Path: "/worktrees/branch"}})
	mem, _ := machineApps(ps, ss)
	for _, app := range mem {
		for _, p := range app.Processes {
			if p.PID == 20 && (p.Attribution == nil || p.Attribution.Proof != "cwd_started_after") {
				t.Fatal("sibling directory missing proof", p)
			}
			if p.PID == 30 && p.Attribution != nil {
				t.Fatal("another checkout attributed", p)
			}
		}
	}
}
