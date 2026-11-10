package collector

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime/metrics"
	"time"

	"github.com/labstack/driga/internal/store"
)

// RuntimeStats is one bounded sample at the end of a daemon process pass.
// Go-managed memory excludes native allocations and is not physical footprint.
// HeapLiveBytes is the live heap at the last completed GC, not a current peak.
type RuntimeStats struct {
	PID                int       `json:"pid"`
	Instance           string    `json:"instance_id"`
	At                 time.Time `json:"observed_at"`
	Phase              string    `json:"phase"`
	HeapLiveBytes      uint64    `json:"heap_live_bytes"`
	HeapGoalBytes      uint64    `json:"heap_goal_bytes"`
	HeapAllocatedBytes uint64    `json:"heap_allocated_bytes"`
	GoSysBytes         uint64    `json:"go_sys_bytes"`
	GoReleasedBytes    uint64    `json:"go_released_bytes"`
	GCCycles           uint64    `json:"gc_cycles"`
	GCCPUSeconds       float64   `json:"gc_cpu_seconds"`
	// GCCPUFraction is cumulative GC CPU divided by Go's available CPU time,
	// including idle time; it is not the collector's process CPU percentage.
	GCCPUFraction float64 `json:"gc_cpu_fraction"`
}

func writeRuntimeStats(tx *store.Tx, at time.Time, instance string) error {
	samples := []metrics.Sample{
		{Name: "/gc/heap/live:bytes"}, {Name: "/gc/heap/goal:bytes"},
		{Name: "/memory/classes/heap/objects:bytes"}, {Name: "/memory/classes/total:bytes"},
		{Name: "/memory/classes/heap/released:bytes"}, {Name: "/gc/cycles/total:gc-cycles"},
		{Name: "/cpu/classes/gc/total:cpu-seconds"}, {Name: "/cpu/classes/total:cpu-seconds"},
	}
	metrics.Read(samples)
	s := RuntimeStats{PID: os.Getpid(), Instance: instance, At: at, Phase: "after_process_pass",
		HeapLiveBytes: samples[0].Value.Uint64(), HeapGoalBytes: samples[1].Value.Uint64(),
		HeapAllocatedBytes: samples[2].Value.Uint64(), GoSysBytes: samples[3].Value.Uint64(),
		GoReleasedBytes: samples[4].Value.Uint64(), GCCycles: samples[5].Value.Uint64(), GCCPUSeconds: samples[6].Value.Float64(),
	}
	if total := samples[7].Value.Float64(); total > 0 {
		s.GCCPUFraction = s.GCCPUSeconds / total
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return tx.SetMeta("collector_runtime", string(raw))
}

// ReadRuntimeStats attributes metadata only to the current writer and cadence.
// A snapshot or a restarted collector cannot borrow an older process's sample.
func ReadRuntimeStats(r interface{ Meta(string) (string, error) }, owner Owner, now time.Time, maxAge time.Duration) (RuntimeStats, error) {
	var s RuntimeStats
	raw, err := r.Meta("collector_runtime")
	if err != nil {
		return s, err
	}
	if len(raw) > 2048 {
		return s, fmt.Errorf("runtime sample too large")
	}
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return s, err
	}
	if owner.PID <= 0 || s.PID != owner.PID || owner.Instance == "" || s.Instance != owner.Instance {
		return RuntimeStats{}, fmt.Errorf("runtime sample belongs to another collector instance")
	}
	if s.At.IsZero() || s.At.After(now) || now.Sub(s.At) > maxAge {
		return RuntimeStats{}, fmt.Errorf("runtime sample is missing or stale")
	}
	if s.Phase != "after_process_pass" || s.GoReleasedBytes > s.GoSysBytes {
		return RuntimeStats{}, fmt.Errorf("invalid runtime sample")
	}
	return s, nil
}
