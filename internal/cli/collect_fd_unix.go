//go:build !windows

package cli

import (
	"os"
	"syscall"
)

// inheritedFile opens a descriptor passed by the parent process. It is marked
// close-on-exec so programs the collector runs do not hold the parent's pipes
// open after the collector exits.
func inheritedFile(fd int, name string) *os.File {
	syscall.CloseOnExec(fd)
	return os.NewFile(uintptr(fd), name)
}
