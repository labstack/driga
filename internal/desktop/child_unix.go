//go:build !windows

package desktop

import (
	"os"
	"os/exec"
	"syscall"
)

// passFiles hands the pipes to the child as descriptors 3 and 4.
func passFiles(cmd *exec.Cmd, notify, parent *os.File) (int, int, error) {
	cmd.ExtraFiles = []*os.File{notify, parent}
	return 3, 4, nil
}

// TerminateProcess asks a collector to shut down as on logout.
func TerminateProcess(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }
