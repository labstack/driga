package derive

import "github.com/labstack/driga/internal/model"

// MemoryUsage describes process accounting, including compressed or swapped
// pages where the platform supplies them. It is not a change in machine RAM.
type MemoryUsage struct {
	Processes int    `json:"processes"`
	Memory    uint64 `json:"memory_bytes"`
	Measure   string `json:"memory_measure"`
	RSS       uint64 `json:"rss_bytes"`
}

func SumProcessMemory(procs []model.Proc) MemoryUsage {
	var usage MemoryUsage
	seen := make(map[int32]bool, len(procs))
	for _, p := range procs {
		if seen[p.PID] {
			continue
		}
		seen[p.PID] = true
		memory, measure := ProcessMemory(p)
		usage.Processes++
		usage.Memory += memory
		usage.RSS += p.RSS
		if usage.Measure == "" {
			usage.Measure = measure
		} else if usage.Measure != measure {
			usage.Measure = "mixed"
		}
	}
	return usage
}

// OrphanProcesses joins a candidate to the latest stored tree. An unrelated
// reused PID or incomplete tree must not contribute a resource estimate.
func OrphanProcesses(idx ProcIndex, o model.Orphan) ([]model.Proc, bool) {
	root, ok := idx.Get(o.RootPID)
	if !ok || root.StartTime.IsZero() || !root.StartTime.Equal(o.StartTime) {
		return nil, false
	}
	current := append([]model.Proc{root}, idx.Descendants(root.PID)...)
	if len(o.PIDs) == 0 {
		return current, true
	}
	expected := make(map[int32]bool, len(o.PIDs))
	for _, pid := range o.PIDs {
		expected[pid] = true
	}
	expected[root.PID] = true
	if len(expected) != len(current) {
		return nil, false
	}
	for _, p := range current {
		if !expected[p.PID] {
			return nil, false
		}
		if start, pinned := o.ProcessStarts[p.PID]; pinned && !p.StartTime.Equal(start) {
			return nil, false
		}
	}
	return current, true
}
