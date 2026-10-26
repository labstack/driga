package platform

import (
	"context"
	"fmt"
	"math"

	"github.com/labstack/driga/internal/model"
	"github.com/shirou/gopsutil/v4/cpu"
)

// machineCPU uses the same OS library as memory/load. No blocking interval or
// package-global baseline: the collector owns the previous successful sample.
func machineCPU(ctx context.Context, s *model.MachineSample) error {
	times, err := cpu.TimesWithContext(ctx, false)
	if err != nil {
		return fmt.Errorf("CPU times: %w", err)
	}
	if len(times) != 1 {
		return fmt.Errorf("CPU times: expected one aggregate")
	}
	t := times[0]
	// Guest time is already included in user/nice on Linux. I/O wait is idle.
	s.CPUTotalSeconds = t.User + t.Nice + t.System + t.Idle + t.Iowait + t.Irq + t.Softirq + t.Steal
	s.CPUBusySeconds = s.CPUTotalSeconds - t.Idle - t.Iowait
	return nil
}

// MachineCPUPercent is busy share across all cores between successful polls.
// The first poll and counter resets are unknown, rather than a misleading zero.
func MachineCPUPercent(prev, cur model.MachineSample) *float64 {
	total, busy := cur.CPUTotalSeconds-prev.CPUTotalSeconds, cur.CPUBusySeconds-prev.CPUBusySeconds
	if prev.CPUTotalSeconds <= 0 || total <= 0 || busy < 0 || busy > total || !cur.TS.After(prev.TS) {
		return nil
	}
	n := 100 * busy / total
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return nil
	}
	return &n
}
