package kill

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
)

// VerifyIdle checks, on demand, what the collector otherwise establishes over
// two passes (spec §7.2): across window the tree keeps its identity and
// membership, accumulates no CPU time, and has no socket peer outside itself.
// It lets snapshot mode offer a kill without the background service.
func VerifyIdle(ctx context.Context, plat platform.Platform, o model.Orphan, expect []int32, window time.Duration) (bool, string, error) {
	return verifyIdle(ctx, plat, o, expect, window, runtime.GOOS == "windows")
}

func verifyIdle(ctx context.Context, plat platform.Platform, o model.Orphan, expect []int32, window time.Duration, windows bool) (bool, string, error) {
	prober, ok := plat.(platform.SocketProber)
	if !ok {
		return false, "cannot check sockets on this platform", nil
	}
	first, reason, err := verifySample(ctx, plat, o, expect)
	if err != nil || reason != "" {
		return false, reason, err
	}
	timer := time.NewTimer(window)
	select {
	case <-ctx.Done():
		timer.Stop()
		return false, "", ctx.Err()
	case <-timer.C:
	}
	second, reason, err := verifySample(ctx, plat, o, expect)
	if err != nil || reason != "" {
		return false, reason, err
	}
	// CPUSeconds has tick granularity; 10ms over the window is noise, not work.
	if windows {
		if reason := windowsTreeChanged(first.tree, second.tree); reason != "" {
			return false, reason, nil
		}
	}
	if grew := second.cpu - first.cpu; grew > 0.01 {
		return false, fmt.Sprintf("used %.2fs CPU in %s", grew, window), nil
	}
	pids := append([]int32{o.RootPID}, expect...)
	peers, err := prober.ExternalPeers(ctx, pids)
	if err != nil {
		return false, "", err
	}
	for _, pid := range pids {
		if _, present := peers[pid]; windows && !present {
			return false, fmt.Sprintf("socket peer check missing PID %d", pid), nil
		}
		if peers[pid] {
			return false, fmt.Sprintf("pid %d has a connection outside the tree", pid), nil
		}
	}
	return true, "", nil
}

// verifySample returns the tree's total CPU seconds, or a reason it no longer
// matches the candidate.
type idleSample struct {
	cpu  float64
	tree []model.Proc
}

func verifySample(ctx context.Context, plat platform.Platform, o model.Orphan, expect []int32) (idleSample, string, error) {
	procs, err := plat.Procs(ctx)
	if err != nil {
		return idleSample{}, "", err
	}
	idx := derive.NewProcIndex(procs)
	root, ok := idx.Get(o.RootPID)
	if !ok || !root.StartTime.Equal(o.StartTime) {
		return idleSample{}, "orphan root changed or exited", nil
	}
	want := make(map[int32]bool, len(expect))
	for _, pid := range expect {
		want[pid] = true
	}
	desc := idx.Descendants(o.RootPID)
	if len(desc) != len(want) {
		return idleSample{}, "orphan descendant set changed", nil
	}
	cpu := root.CPUSeconds
	for _, p := range desc {
		if !want[p.PID] {
			return idleSample{}, "orphan descendant set changed", nil
		}
		cpu += p.CPUSeconds
	}
	return idleSample{cpu: cpu, tree: append(desc, root)}, "", nil
}
