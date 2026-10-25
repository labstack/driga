package desktop

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// passFiles hands the pipes to the child as inherited handles; Windows has no
// numbered descriptors beyond the standard three, so the child receives the
// handle values. The collector runs without a console window.
func passFiles(cmd *exec.Cmd, notify, parent *os.File) (int, int, error) {
	handles := []syscall.Handle{syscall.Handle(notify.Fd()), syscall.Handle(parent.Fd())}
	for _, h := range handles {
		if err := syscall.SetHandleInformation(h, syscall.HANDLE_FLAG_INHERIT, syscall.HANDLE_FLAG_INHERIT); err != nil {
			return 0, 0, err
		}
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW, AdditionalInheritedHandles: handles}
	return int(handles[0]), int(handles[1]), nil
}

// TerminateProcess ends a collector. Windows has no termination signal a
// console-less process can receive, so it is killed.
func TerminateProcess(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}

// hideWindow runs a console program without opening a console window.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
}
