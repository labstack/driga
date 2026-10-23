package main

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// Match the no-console, piped launch used by Claude's MCP client. An eager
// terminal background query used to block package initialization in this mode.
func configureStartupCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
}
