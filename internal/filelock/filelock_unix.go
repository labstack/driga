//go:build !windows

// Package filelock provides exclusive advisory locks released when files close.
package filelock

import (
	"errors"
	"os"
	"syscall"
)

func Lock(f *os.File, nonblocking bool) error {
	flags := syscall.LOCK_EX
	if nonblocking {
		flags |= syscall.LOCK_NB
	}
	return syscall.Flock(int(f.Fd()), flags)
}
func Unlock(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
func Busy(err error) bool {
	return errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)
}
