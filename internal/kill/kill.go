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

// Result records graceful attempts and processes still alive after the wait.
// Force escalates to SIGKILL on Unix or TerminateProcess on Windows.
type Result struct {
	Signaled     []int32            `json:"signaled"`
	Stopped      []int32            `json:"stopped"`
	Survivors    []int32            `json:"survivors"`
	Unknown      []int32            `json:"unknown"`
	Before       derive.MemoryUsage `json:"before"`
	StoppedUsage derive.MemoryUsage `json:"stopped_usage"`
	Aborted      bool               `json:"aborted"`
	Reason       string             `json:"reason,omitempty"`
}

// Option adjusts how a tree is revalidated.
type Option func(*options)

type options struct{ classifier *derive.Classifier }

// WithClassifier lets the root's signature set IdleWithoutClients. Without it,
// any listener or system-service socket keeps the tree.
func WithClassifier(c *derive.Classifier) Option {
	return func(o *options) { o.classifier = c }
}

func collectOptions(opts []Option) options {
	var o options
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	return o
}

// Tree checks identities and expected descendants, then attempts graceful
// shutdown deepest first. A nonpositive wait uses the five-second default.
func Tree(ctx context.Context, plat platform.Platform, o model.Orphan, expect []int32, force bool, wait time.Duration, opts ...Option) (Result, error) {
	return runTree(ctx, plat, o, expect, force, wait, signalSnapshotIdentity, runtime.GOOS == "windows", collectOptions(opts))
}

func tree(ctx context.Context, plat platform.Platform, o model.Orphan, expect []int32, force bool, wait time.Duration, signal func(int, syscall.Signal) error, opts ...Option) (Result, error) {
	return runTree(ctx, plat, o, expect, force, wait, func(p model.Proc, sig syscall.Signal) error { return signal(int(p.PID), sig) }, false, collectOptions(opts))
}

func runTree(ctx context.Context, plat platform.Platform, o model.Orphan, expect []int32, force bool, wait time.Duration, signal func(model.Proc, syscall.Signal) error, windows bool, opt options) (result Result, retErr error) {
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
	if windows || len(o.ProcessStarts) > 0 {
		for _, p := range targets {
			if p.StartTime.IsZero() || o.ProcessStarts[p.PID].IsZero() {
				return abort("tree process identity unavailable; collect and list again")
			}
			if !p.StartTime.Equal(o.ProcessStarts[p.PID]) {
				return abort("orphan member start time changed (PID reused)")
			}
		}
	}
	if windows {
		if reason := windowsTreeChanged(targets, procs); reason != "" {
			return abort(reason)
		}
		parent, present := idx.Get(root.PPID)
		if present && (parent.StartTime.IsZero() || !parent.StartTime.After(root.StartTime)) {
			return abort("orphan parent is still alive or unknown")
		}
	}
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
		peers, err := platform.ProbePeers(ctx, prober, pids, derive.NeedsResolvedPeers(opt.classifier, targets))
		if err != nil {
			return Result{Aborted: true, Reason: "socket peer check failed"}, fmt.Errorf("check socket peers: %w", err)
		}
		for _, p := range targets {
			if state, present := peers[p.PID]; !present {
				return abort(fmt.Sprintf("socket peer check missing PID %d", p.PID))
			} else if derive.InUse(opt.classifier, root, p, state) {
				return abort(peerReason(p.PID, state))
			}
		}
	}
	if _, ok := plat.(platform.SocketProber); windows && !ok {
		return abort("socket peer inspection unavailable")
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
	result.Before = derive.SumProcessMemory(targets)
	// A failed liveness lookup is not proof of exit. Observe the complete process
	// table once more and compare exact identities before reporting stopped usage.
	defer func() {
		observeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		current, observeErr := plat.Procs(observeCtx)
		result.Stopped, result.Survivors, result.Unknown = []int32{}, []int32{}, []int32{}
		if observeErr != nil {
			for _, p := range targets {
				result.Unknown = append(result.Unknown, p.PID)
			}
			retErr = errors.Join(retErr, fmt.Errorf("final process observation: %w", observeErr))
			return
		}
		idx := derive.NewProcIndex(current)
		var stopped []model.Proc
		for _, p := range targets {
			now, present := idx.Get(p.PID)
			switch {
			case present && now.StartTime.IsZero() && !now.Zombie:
				result.Unknown = append(result.Unknown, p.PID)
			case present && now.StartTime.Equal(p.StartTime) && !now.Zombie:
				result.Survivors = append(result.Survivors, p.PID)
			default:
				result.Stopped = append(result.Stopped, p.PID)
				stopped = append(stopped, p)
			}
		}
		result.StoppedUsage = derive.SumProcessMemory(stopped)
	}()
	var errs []error
	checkTree := func() (string, error) {
		if !windows {
			return "", nil
		}
		current, err := plat.Procs(ctx)
		if err != nil {
			return "", err
		}
		return windowsTreeChanged(targets, current), nil
	}
	for _, p := range targets {
		if err := ctx.Err(); err != nil {
			return result, errors.Join(append(errs, err)...)
		}
		if reason, err := checkTree(); err != nil {
			return result, err
		} else if reason != "" {
			result.Aborted, result.Reason = true, reason
			return result, errors.Join(errs...)
		}
		if !plat.Alive(p.PID, p.StartTime) {
			continue
		}
		if err := signal(p, syscall.SIGTERM); err != nil {
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
	waitForExit := func() error {
		deadline := time.Now().Add(wait)
		for {
			if reason, err := checkTree(); err != nil {
				return err
			} else if reason != "" {
				result.Aborted, result.Reason = true, reason
				return nil
			}
			result.Survivors = nil
			for _, p := range targets {
				if plat.Alive(p.PID, p.StartTime) {
					result.Survivors = append(result.Survivors, p.PID)
				}
			}
			if len(result.Survivors) == 0 || !time.Now().Before(deadline) {
				return nil
			}
			timer := time.NewTimer(min(100*time.Millisecond, time.Until(deadline)))
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	if err := waitForExit(); err != nil || result.Aborted {
		return result, errors.Join(append(errs, err)...)
	}
	if force {
		for _, pid := range result.Survivors {
			if err := ctx.Err(); err != nil {
				return result, errors.Join(append(errs, err)...)
			}
			p, _ := idx.Get(pid)
			// Recheck identity so a reused survivor PID cannot be escalated.
			if reason, err := checkTree(); err != nil {
				return result, err
			} else if reason != "" {
				result.Aborted, result.Reason = true, reason
				return result, errors.Join(errs...)
			}
			if !plat.Alive(pid, p.StartTime) {
				continue
			}
			if err := signal(p, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
				errs = append(errs, fmt.Errorf("SIGKILL %d: %w", pid, err))
			}
		}
		if err := waitForExit(); err != nil || result.Aborted {
			return result, errors.Join(append(errs, err)...)
		}
	}
	return result, errors.Join(errs...)
}
