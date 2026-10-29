//go:build !windows

package kill

import (
	"context"
	"errors"
	"fmt"
	"github.com/labstack/driga/internal/model"
	"github.com/shirou/gopsutil/v4/process"
	"os"
	"runtime"
	"strings"
	"syscall"
	"time"
)

var signalProcess = syscall.Kill

func signalIdentity(p model.Proc, sig syscall.Signal) error { return signalProcess(int(p.PID), sig) }

// Linux pidfds pin the kernel object across inspection and signal. macOS and
// older Linux kernels have a residual recheck-to-kill race: Unix offers no
// portable checked-process handle. Never use tolerant liveness as authority.
func signalSnapshotIdentity(p model.Proc, sig syscall.Signal) error {
	fd := -1
	if runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64") {
		value, _, e := syscall.Syscall(434, uintptr(p.PID), 0, 0) // pidfd_open
		if e == 0 {
			fd = int(value)
			defer syscall.Close(fd)
		} else if e != syscall.ENOSYS && e != syscall.EINVAL {
			return e
		}
	}
	ctx := context.Background()
	proc, e := process.NewProcessWithContext(ctx, p.PID)
	if e != nil {
		if errors.Is(e, process.ErrorProcessNotRunning) || errors.Is(e, syscall.ESRCH) || os.IsNotExist(e) {
			return syscall.ESRCH
		}
		return e
	}
	start, e := proc.CreateTimeWithContext(ctx)
	if e != nil {
		return e
	}
	uids, e := proc.UidsWithContext(ctx)
	if e != nil || len(uids) == 0 {
		return fmt.Errorf("process owner query failed")
	}
	argv, e := proc.CmdlineSliceWithContext(ctx)
	if e != nil {
		return e
	}
	exe, e := proc.ExeWithContext(ctx)
	if e != nil {
		return e
	}
	now := model.Proc{PID: p.PID, StartTime: time.UnixMilli(start), OwnerUID: uint32(uids[0]), OwnerKnown: true, Exe: exe, Argv: argv, Cmdline: strings.Join(argv, " ")}
	if !Owned(now) || !SameIdentity(p, now) {
		return errSnapshotIdentityChanged
	}
	if fd >= 0 {
		_, _, e := syscall.Syscall6(424, uintptr(fd), uintptr(sig), 0, 0, 0, 0)
		if e != 0 {
			return e
		}
		return nil
	} // pidfd_send_signal
	return signalIdentity(p, sig)
}
