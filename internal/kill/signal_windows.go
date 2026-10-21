package kill

import (
	"fmt"
	"syscall"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
	"golang.org/x/sys/windows"
)

// Hold the checked handle through termination so PID reuse cannot redirect it.
// Console group membership is not exposed by Toolhelp: never infer a group ID
// from a PID or attach and broadcast to somebody else's console.
func signalIdentity(p model.Proc, sig syscall.Signal) error {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE, false, uint32(p.PID))
	if err != nil {
		if err == windows.ERROR_INVALID_PARAMETER {
			return syscall.ESRCH
		}
		return err
	}
	defer windows.CloseHandle(h)
	var created, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &created, &exit, &kernel, &user); err != nil {
		return err
	}
	if p.StartTime.IsZero() || created.Nanoseconds() != p.StartTime.UnixNano() {
		return fmt.Errorf("PID %d start time changed", p.PID)
	}
	alive := func() bool { var code uint32; return windows.GetExitCodeProcess(h, &code) == nil && code == 259 }
	if !alive() {
		return syscall.ESRCH
	}
	if sig == syscall.SIGKILL {
		return windows.TerminateProcess(h, 1)
	}
	return platform.GracefulStop(h, uint32(p.PID), false)
}
