package platform

import (
	"fmt"
	"golang.org/x/sys/windows"
	"time"
)

// ProcessStart opens only the identity query handle and reads kernel times.
func ProcessStart(pid int32) (time.Time, error) {
	if pid <= 0 {
		return time.Time{}, fmt.Errorf("invalid process identity")
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return time.Time{}, err
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return time.Time{}, err
	}
	if code != 259 {
		return time.Time{}, fmt.Errorf("process exited")
	}
	var start, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &start, &exit, &kernel, &user); err != nil {
		return time.Time{}, err
	}
	return winTime(winTicks(start)), nil
}
