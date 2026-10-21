// Package kill revalidates orphan identities before terminating their trees.
package kill

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sort"
	"syscall"
	"time"

	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
)

// Result records successful SIGTERMs and processes still alive after the wait.
// Survivors are also sent SIGKILL when force is enabled.
type Result struct {
	Signaled, Survivors []int32
	Aborted             bool
	Reason              string
}

// Tree checks the root's identity and expected descendant set, then sends
// SIGTERM deepest first. A nonpositive wait uses the five-second default.
func Tree(ctx context.Context, plat platform.Platform, o model.Orphan, expect []int32, force bool, wait time.Duration) (Result, error) {
	if runtime.GOOS == "windows" {
		return Result{}, platform.ErrWindowsAction
	}
	return tree(ctx, plat, o, expect, force, wait, signalProcess)
}

func tree(ctx context.Context, plat platform.Platform, o model.Orphan, expect []int32, force bool, wait time.Duration, signal func(int, syscall.Signal) error) (Result, error) {
	abort := func(reason string) (Result, error) { return Result{Aborted: true, Reason: reason}, nil }
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if o.Young {
		return abort("too young to kill")
	}
	procs, err := plat.Procs(ctx)
	if err != nil {
		return Result{}, err
	}
	idx := derive.NewProcIndex(procs)
	root, ok := idx.Get(o.RootPID)
	if !ok {
		return abort("orphan root no longer exists")
	}
	// Both timestamps came from the kernel. Registry-write skew does not
	// apply to the process identity pinned when this candidate was listed.
	if !root.StartTime.Equal(o.StartTime) {
		return abort("orphan root start time changed (PID reused)")
	}
	desc := idx.Descendants(root.PID)
	expected := make(map[int32]bool, len(expect))
	for _, pid := range expect {
		expected[pid] = true
	}
	if len(desc) != len(expected) {
		return abort("orphan descendant set changed")
	}
	for _, p := range desc {
		if !expected[p.PID] {
			return abort("orphan descendant set changed")
		}
	}
	// Validate the entire target set before sending even the first signal.
	protected := map[int32]bool{int32(os.Getpid()): true, int32(os.Getppid()): true}
	for _, pid := range idx.Ancestors(int32(os.Getpid())) {
		protected[pid] = true
	}
	for _, pid := range idx.Ancestors(int32(os.Getppid())) {
		protected[pid] = true
	}
	targets := append(desc, root)
	for _, p := range targets {
		if p.PID <= 1 || protected[p.PID] {
			return abort(fmt.Sprintf("refusing to signal protected PID %d", p.PID))
		}
	}
	if prober, ok := plat.(platform.SocketProber); ok {
		pids := make([]int32, 0, len(targets))
		for _, p := range targets {
			pids = append(pids, p.PID)
		}
		peers, err := prober.ExternalPeers(ctx, pids)
		if err != nil {
			return Result{Aborted: true, Reason: "socket peer check failed"}, fmt.Errorf("check socket peers: %w", err)
		}
		for _, pid := range pids {
			if active, present := peers[pid]; !present {
				return abort(fmt.Sprintf("socket peer check missing PID %d", pid))
			} else if active {
				return abort(fmt.Sprintf("PID %d has an external peer or listening socket", pid))
			}
		}
	}
	depth := make(map[int32]int, len(targets))
	for _, p := range targets {
		depth[p.PID] = len(idx.Ancestors(p.PID))
	}
	sort.Slice(targets, func(i, j int) bool {
		if depth[targets[i].PID] == depth[targets[j].PID] {
			return targets[i].PID < targets[j].PID
		}
		return depth[targets[i].PID] > depth[targets[j].PID]
	})
	var result Result
	var errs []error
	for _, p := range targets {
		if err := ctx.Err(); err != nil {
			return result, errors.Join(append(errs, err)...)
		}
		if !plat.Alive(p.PID, p.StartTime) {
			continue
		}
		if err := signal(int(p.PID), syscall.SIGTERM); err != nil {
			if !errors.Is(err, syscall.ESRCH) {
				errs = append(errs, fmt.Errorf("SIGTERM %d: %w", p.PID, err))
			}
		} else {
			result.Signaled = append(result.Signaled, p.PID)
		}
	}
	if wait <= 0 {
		wait = 5 * time.Second
	}
	deadline := time.Now().Add(wait)
	for {
		result.Survivors = nil
		for _, p := range targets {
			if plat.Alive(p.PID, p.StartTime) {
				result.Survivors = append(result.Survivors, p.PID)
			}
		}
		if len(result.Survivors) == 0 || !time.Now().Before(deadline) {
			break
		}
		timer := time.NewTimer(min(100*time.Millisecond, time.Until(deadline)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return result, errors.Join(append(errs, ctx.Err())...)
		case <-timer.C:
		}
	}
	if force {
		for _, pid := range result.Survivors {
			if err := ctx.Err(); err != nil {
				return result, errors.Join(append(errs, err)...)
			}
			p, _ := idx.Get(pid)
			// Recheck identity so a reused survivor PID cannot be escalated.
			if !plat.Alive(pid, p.StartTime) {
				continue
			}
			if err := signal(int(pid), syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
				errs = append(errs, fmt.Errorf("SIGKILL %d: %w", pid, err))
			}
		}
	}
	return result, errors.Join(errs...)
}
