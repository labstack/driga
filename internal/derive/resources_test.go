package derive

import (
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestSumProcessMemoryMeasuresAndDeduplicates(t *testing.T) {
	procs := []model.Proc{
		{PID: 10, Footprint: 100, RSS: 10, MemoryMeasure: "physical_footprint"},
		{PID: 11, RSS: 20, Swap: 30, MemoryMeasure: "rss_swap"},
		{PID: 12, RSS: 40},
	}
	usage := SumProcessMemory(append(procs, procs[0]))
	if usage.Processes != 3 || usage.Memory != 190 || usage.RSS != 70 || usage.Measure != "mixed" {
		t.Fatalf("usage=%+v", usage)
	}
}

func TestOrphanResourcesRejectChangedIdentitiesAndMembership(t *testing.T) {
	start := time.Unix(1700000000, 0)
	root := model.Proc{PID: 100, PPID: 1, StartTime: start}
	child := model.Proc{PID: 101, PPID: 100, StartTime: start}
	o := model.Orphan{RootPID: 100, StartTime: start, PIDs: []int32{100, 101}, ProcessStarts: map[int32]time.Time{101: start}}
	for _, scenario := range []string{"valid", "reused-root", "reused-child", "missing-child", "new-child", "unknown-root"} {
		t.Run(scenario, func(t *testing.T) {
			procs := []model.Proc{root, child}
			switch scenario {
			case "reused-root":
				procs[0].StartTime = start.Add(time.Second)
			case "reused-child":
				procs[1].StartTime = start.Add(time.Second)
			case "missing-child":
				procs = procs[:1]
			case "new-child":
				procs = append(procs, model.Proc{PID: 102, PPID: 100, StartTime: start})
			case "unknown-root":
				procs[0].StartTime = time.Time{}
			}
			members, known := OrphanProcesses(NewProcIndex(procs), o)
			if known != (scenario == "valid") || known && len(members) != 2 {
				t.Fatalf("known=%t members=%+v", known, members)
			}
		})
	}
}
