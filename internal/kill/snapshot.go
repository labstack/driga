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

// ProtectedReason is why p may not be signalled given one live snapshot and
// the broker's protected roots, or "" when it may.
func ProtectedReason(p model.Proc, live, protected []model.Proc) string {
	if reason := identityReason(p); reason != "" {
		return reason
	}
	return NewProtection(live, protected).Reason(p)
}

// Protection is the set of processes one live snapshot protects: Driga, Chat
// and the broker's protected roots, all their descendants, and the ancestors
// of all of them. It errs toward protecting: descendants follow every parent
// link, and an ancestor walk stops only at a parent that provably reused an
// exited parent's PID on Windows, where a process keeps its creator's PID.
type Protection struct{ blocked map[int32]bool }

// NewProtection computes the protected set once, so callers checking many
// targets against one snapshot do not rebuild it per target.
func NewProtection(live, protected []model.Proc) Protection {
	return newProtection(live, protected, runtime.GOOS == "windows")
}

func newProtection(live, protected []model.Proc, windows bool) Protection {
	byPID := make(map[int32]model.Proc, len(live))
	children := map[int32][]int32{}
	for _, r := range live {
		byPID[r.PID] = r
		children[r.PPID] = append(children[r.PPID], r.PID)
	}
	blocked := map[int32]bool{}
	ancestors := func(pid int32) {
		for seen := map[int32]bool{pid: true}; ; {
			cur, ok := byPID[pid]
			if !ok || cur.PPID <= 0 || seen[cur.PPID] {
				return
			}
			parent, ok := byPID[cur.PPID]
			if ok && windows && !parent.StartTime.IsZero() && !cur.StartTime.IsZero() && parent.StartTime.After(cur.StartTime) {
				return
			}
			pid = cur.PPID
			seen[pid] = true
			blocked[pid] = true
		}
	}
	rooted := map[int32]bool{}
	root := func(pid int32) {
		if rooted[pid] {
			return
		}
		blocked[pid] = true
		ancestors(pid)
		for pending := []int32{pid}; len(pending) > 0; {
			parent := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			rooted[parent] = true
			for _, child := range children[parent] {
				if !rooted[child] {
					blocked[child] = true
					pending = append(pending, child)
				}
			}
		}
	}
	self := int32(os.Getpid())
	blocked[self] = true
	if _, ok := byPID[self]; ok {
		ancestors(self)
	} else {
		// Without itself in the snapshot, Driga still protects its parent and
		// whatever of the parent's ancestry the snapshot has.
		parent := int32(os.Getppid())
		blocked[parent] = true
		ancestors(parent)
	}
	for _, r := range protected {
		// A newer process holding the PID of a broker root that exited is
		// not that root. Only a clearly later start proves it; any other
		// mismatch keeps the protection.
		if now, ok := byPID[r.PID]; ok && !r.StartTime.IsZero() && !now.StartTime.IsZero() && now.StartTime.After(r.StartTime.Add(time.Second)) {
			continue
		}
		root(r.PID)
	}
	for _, r := range live {
		name := strings.ToLower(filepath.Base(strings.ReplaceAll(r.Exe, "\\", "/")))
		if name == "driga" || name == "driga.exe" || strings.HasPrefix(name, "driga-") || r.Role == "chat" || strings.Contains(r.Cmdline, model.ChatPreamble) {
			root(r.PID)
		}
	}
	return Protection{blocked: blocked}
}

// Reason is why p may not be signalled, or "" when it may.
func (pr Protection) Reason(p model.Proc) string {
	if reason := identityReason(p); reason != "" {
		return reason
	}
	if pr.blocked[p.PID] {
		return "Driga, Chat, or an ancestor/descendant is protected"
	}
	return ""
}

// identityReason refuses a target whose own identity rules out a signal,
// before any process graph is built.
func identityReason(p model.Proc) string {
	if p.PID <= 1 {
		return "system PID or process group is refused"
	}
	if !Owned(p) {
		return "process owner is unknown or differs from your user"
	}
	if p.Partial || p.StartTime.IsZero() || p.Exe == "" || p.Cmdline == "" || len(p.Argv) == 0 {
		return "process identity is incomplete"
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
