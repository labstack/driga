//go:build darwin || linux

package platform

import (
	"os/exec"
	"syscall"
	"time"
)

// OwnChild places only the launched agent and its descendants in a new process
// group. Stop allows a short shutdown grace, then reaps that owned group.
func OwnChild(cmd *exec.Cmd) func() {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = 2 * time.Second
	return func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}
}
