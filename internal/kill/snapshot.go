package kill

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
)

type SnapshotResult struct {
	Stopped, Vanished, Skipped, Survivors []model.Proc
	Reasons                               map[int32]string
}

// errSnapshotIdentityChanged means the signal was safely withheld because the
// target no longer matches its approved identity, rather than a signal failure.
var errSnapshotIdentityChanged = errors.New("process identity changed before signal")

// SameIdentity deliberately does not use the collector's tolerant Alive or
// SameStart helpers. These are live kernel samples, not registry timestamps.
func SameIdentity(a, b model.Proc) bool {
	return a.PID == b.PID && !a.StartTime.IsZero() && a.StartTime.Equal(b.StartTime) && a.OwnerKnown && b.OwnerKnown && a.OwnerUID == b.OwnerUID && a.OwnerSID == b.OwnerSID && a.Exe != "" && a.Exe == b.Exe && a.Cmdline != "" && a.Cmdline == b.Cmdline && len(a.Argv) > 0 && slices.Equal(a.Argv, b.Argv) && !a.Partial && !b.Partial
}
func Owned(p model.Proc) bool {
	if !p.OwnerKnown {
		return false
	}
	if runtime.GOOS == "windows" {
		u, e := user.Current()
		return e == nil && p.OwnerSID != "" && p.OwnerSID == u.Uid
	}
	return p.OwnerUID == uint32(os.Getuid()) && p.OwnerSID == ""
}

// ProtectedReason uses live graph membership on each call, including new Chat
// descendants and ancestors. The broker supplies its complete protected roots.
func ProtectedReason(p model.Proc, live, protected []model.Proc) string {
	if p.PID <= 1 {
		return "system PID or process group is refused"
	}
	if !Owned(p) {
		return "process owner is unknown or differs from your user"
	}
	if p.Partial || p.StartTime.IsZero() || p.Exe == "" || p.Cmdline == "" || len(p.Argv) == 0 {
		return "process identity is incomplete"
	}
	roots := map[int32]bool{}
	blocked := map[int32]bool{int32(os.Getpid()): true, int32(os.Getppid()): true}
	for _, r := range protected {
		roots[r.PID] = true
		blocked[r.PID] = true
	}
	for _, r := range live {
		name := strings.ToLower(filepath.Base(strings.ReplaceAll(r.Exe, "\\", "/")))
		if name == "driga" || name == "driga.exe" || strings.HasPrefix(name, "driga-") || r.Role == "chat" || strings.Contains(r.Cmdline, model.ChatPreamble) {
			roots[r.PID] = true
			blocked[r.PID] = true
		}
	}
	// Ancestors are protected, but their unrelated descendants are not. Only
	// explicit Chat/Driga roots propagate protection downwards.
	for changed := true; changed; {
		changed = false
		for _, r := range live {
			if roots[r.PPID] && !roots[r.PID] {
				roots[r.PID] = true
				blocked[r.PID] = true
				changed = true
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for _, r := range live {
			if blocked[r.PID] && r.PPID > 0 && !blocked[r.PPID] {
				blocked[r.PPID] = true
				changed = true
			}
		}
	}
	if blocked[p.PID] {
		return "Driga, Chat, or an ancestor/descendant is protected"
	}
	return ""
}

func Snapshot(ctx context.Context, plat platform.Platform, targets, protected []model.Proc, force bool, wait time.Duration) (SnapshotResult, error) {
	return snapshot(ctx, plat, targets, protected, force, wait, signalSnapshotIdentity)
}

func snapshot(ctx context.Context, plat platform.Platform, targets, protected []model.Proc, force bool, wait time.Duration, signal func(model.Proc, syscall.Signal) error) (SnapshotResult, error) {
	r := SnapshotResult{Reasons: make(map[int32]string)}
	var pending []model.Proc
	var errs []error
	seen := map[int32]bool{}
	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	find := func(ps []model.Proc, pid int32) (model.Proc, bool) {
		for _, p := range ps {
			if p.PID == pid {
				return p, true
			}
		}
		return model.Proc{}, false
	}
	for _, p := range targets {
		if seen[p.PID] {
			continue
		}
		seen[p.PID] = true
		p.Argv = append([]string(nil), p.Argv...)
		if e := ctx.Err(); e != nil {
			return r, errors.Join(append(errs, e)...)
		}
		live, e := plat.Procs(ctx)
		if e != nil {
			return r, errors.Join(append(errs, e)...)
		}
		now, ok := find(live, p.PID)
		if !ok || now.Zombie {
			r.Vanished = append(r.Vanished, p)
			r.Reasons[p.PID] = "already vanished"
			continue
		}
		reason := ProtectedReason(now, live, protected)
		if reason == "" && !SameIdentity(p, now) {
			reason = "process identity changed"
		}
		if reason != "" {
			r.Skipped = append(r.Skipped, p)
			r.Reasons[p.PID] = reason
			continue
		}
		// The platform signal path performs a second fresh kernel identity check;
		// stable handles retain that checked object through signalling where possible.
		e = signal(p, sig)
		switch {
		case errors.Is(e, syscall.ESRCH):
			r.Vanished = append(r.Vanished, p)
			r.Reasons[p.PID] = "vanished before signal"
		case e != nil:
			r.Skipped = append(r.Skipped, p)
			r.Reasons[p.PID] = e.Error()
			if !errors.Is(e, errSnapshotIdentityChanged) {
				errs = append(errs, fmt.Errorf("signal PID %d: %w", p.PID, e))
			}
		default:
			pending = append(pending, p)
		}
	}
	if wait <= 0 {
		wait = 5 * time.Second
	}
	deadline := time.Now().Add(wait)
	for len(pending) > 0 {
		if e := ctx.Err(); e != nil {
			r.Survivors = append(r.Survivors, pending...)
			return r, errors.Join(append(errs, e)...)
		}
		live, e := plat.Procs(ctx)
		if e != nil {
			r.Survivors = append(r.Survivors, pending...)
			return r, errors.Join(append(errs, e)...)
		}
		remaining := make([]model.Proc, 0, len(pending))
		for _, p := range pending {
			now, ok := find(live, p.PID)
			if !ok || now.Zombie {
				r.Stopped = append(r.Stopped, p)
				r.Reasons[p.PID] = "stopped"
			} else if !SameIdentity(p, now) {
				r.Skipped = append(r.Skipped, p)
				r.Reasons[p.PID] = "identity changed after signal"
			} else {
				remaining = append(remaining, p)
			}
		}
		pending = remaining
		if len(pending) == 0 || !time.Now().Before(deadline) {
			break
		}
		timer := time.NewTimer(min(20*time.Millisecond, time.Until(deadline)))
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
	for _, p := range pending {
		r.Survivors = append(r.Survivors, p)
		r.Reasons[p.PID] = "survived signal; force needs a separate approval"
	}
	return r, errors.Join(errs...)
}
