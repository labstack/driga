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
// membership, no process uses more than background CPU, and no process has a
// socket peer outside the tree.
// It lets snapshot mode offer a kill without the background service.
func VerifyIdle(ctx context.Context, plat platform.Platform, o model.Orphan, expect []int32, window time.Duration, opts ...Option) (bool, string, error) {
	return verifyIdle(ctx, plat, o, expect, window, runtime.GOOS == "windows", opts...)
}

func verifyIdle(ctx context.Context, plat platform.Platform, o model.Orphan, expect []int32, window time.Duration, windows bool, opts ...Option) (bool, string, error) {
	return verifyIdlePinned(ctx, plat, o, expect, window, windows, nil, opts...)
}

// VerifyForAction pins every member observed across a fresh idle window. The
// caller passes this candidate to Tree immediately after consent.
func VerifyForAction(ctx context.Context, plat platform.Platform, o model.Orphan, expect []int32, window time.Duration, opts ...Option) (model.Orphan, error) {
	var identities map[int32]model.Proc
	ok, reason, err := verifyIdlePinned(ctx, plat, o, expect, window, runtime.GOOS == "windows", &identities, opts...)
	if err != nil {
		return o, err
	}
	if !ok {
		return o, fmt.Errorf("not idle: %s", reason)
	}
	o.ProcessIdentities = identities
	o.ProcessStarts = make(map[int32]time.Time, len(identities))
	for pid, p := range identities {
		o.ProcessStarts[pid] = p.StartTime
	}
	return o, nil
}

func verifyIdlePinned(ctx context.Context, plat platform.Platform, o model.Orphan, expect []int32, window time.Duration, windows bool, pinned *map[int32]model.Proc, opts ...Option) (bool, string, error) {
	opt := collectOptions(opts)
	prober, ok := plat.(platform.SocketProber)
	if !ok {
		return false, "cannot check sockets on this platform", nil
	}
	first, reason, err := verifySample(ctx, plat, o, expect)
	if err != nil || reason != "" {
		return false, reason, err
	}
	starts := make(map[int32]time.Time, len(first.tree))
	identities := make(map[int32]model.Proc, len(first.tree))
	for _, p := range first.tree {
		starts[p.PID] = p.StartTime
		p.Argv = append([]string(nil), p.Argv...)
		identities[p.PID] = p
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
	if windows {
		if reason := windowsTreeChanged(first.tree, second.tree); reason != "" {
			return false, reason, nil
		}
	}
	for _, p := range second.tree {
		if !starts[p.PID].Equal(p.StartTime) {
			return false, fmt.Sprintf("pid %d identity changed during idle verification", p.PID), nil
		}
		if pinned != nil && (!Owned(p) || !SameIdentity(identities[p.PID], p)) {
			return false, fmt.Sprintf("pid %d identity unavailable or changed during idle verification", p.PID), nil
		}
	}
	// CPUSeconds has tick granularity, so each process gets 10ms of slack on
	// top of the background share of the window, and the tree as a whole gets
	// 10ms on top of its larger share.
	budget := 0.01 + window.Seconds()*derive.BackgroundCPUPercent/100
	var total float64
	for pid, after := range second.cpu {
		grew := after - first.cpu[pid]
		if grew < 0 {
			return false, fmt.Sprintf("pid %d CPU counter went backwards", pid), nil
		}
		if grew > budget {
			return false, fmt.Sprintf("pid %d used %.2fs CPU in %s", pid, grew, window), nil
		}
		total += grew
	}
	if total > 0.01+window.Seconds()*derive.BackgroundTreeCPUPercent/100 {
		return false, fmt.Sprintf("tree used %.2fs CPU in %s", total, window), nil
	}
	pids := make([]int32, 0, len(second.tree))
	for _, p := range second.tree {
		pids = append(pids, p.PID)
	}
	root := second.tree[len(second.tree)-1]
	peers, err := platform.ProbePeers(ctx, prober, pids, derive.NeedsResolvedPeers(opt.classifier, second.tree))
	if err != nil {
		return false, "", err
	}
	for _, p := range second.tree {
		state, present := peers[p.PID]
		if windows && !present {
			return false, fmt.Sprintf("socket peer check missing PID %d", p.PID), nil
		}
		if derive.InUse(opt.classifier, root, p, state) {
			return false, peerReason(p.PID, state), nil
		}
	}
	if pinned != nil {
		*pinned = identities
	}
	return true, "", nil
}

// peerReason says why a tree member's socket state keeps the tree.
func peerReason(pid int32, state model.PeerState) string {
	switch state {
	case model.PeerListening:
		return fmt.Sprintf("pid %d has a listening socket", pid)
	case model.PeerSystem:
		return fmt.Sprintf("pid %d is connected to a system service", pid)
	}
	return fmt.Sprintf("pid %d has a connection outside the tree", pid)
}

// verifySample returns each tree member's CPU seconds, or a reason the tree no
// longer matches the candidate.
type idleSample struct {
	cpu  map[int32]float64
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
	for _, p := range append(desc, root) {
		if o.ProcessIdentities != nil && (!Owned(p) || !SameIdentity(o.ProcessIdentities[p.PID], p)) {
			return idleSample{}, fmt.Sprintf("pid %d identity changed since candidate was selected", p.PID), nil
		}
		if start, pinned := o.ProcessStarts[p.PID]; pinned && (start.IsZero() || !p.StartTime.Equal(start)) {
			return idleSample{}, fmt.Sprintf("pid %d identity changed since candidate was collected", p.PID), nil
		}
	}
	if len(desc) != len(want) {
		return idleSample{}, "orphan descendant set changed", nil
	}
	cpu := map[int32]float64{root.PID: root.CPUSeconds}
	for _, p := range desc {
		if !want[p.PID] {
			return idleSample{}, "orphan descendant set changed", nil
		}
		cpu[p.PID] = p.CPUSeconds
	}
	return idleSample{cpu: cpu, tree: append(desc, root)}, "", nil
}
