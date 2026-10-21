package filelock

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// Lock a byte beyond file contents: Windows locks are mandatory, so locking
// byte zero would deny readers access to the collector owner's PID.
func lockOffset() windows.Overlapped {
	return windows.Overlapped{Offset: 0xfffffffe, OffsetHigh: 0x7fffffff}
}
func Lock(f *os.File, nonblocking bool) error {
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK)
	if nonblocking {
		flags |= windows.LOCKFILE_FAIL_IMMEDIATELY
	}
	o := lockOffset()
	return windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, &o)
}
func Unlock(f *os.File) error {
	o := lockOffset()
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &o)
}
func Busy(err error) bool { return errors.Is(err, windows.ERROR_LOCK_VIOLATION) }
