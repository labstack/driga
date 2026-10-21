package platform

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var actionUser32 = windows.NewLazySystemDLL("user32.dll")
var actionEnumWindows = actionUser32.NewProc("EnumWindows")
var actionWindowPID = actionUser32.NewProc("GetWindowThreadProcessId")
var actionPostMessage = actionUser32.NewProc("PostMessageW")

type windowCloseRequest struct {
	handle windows.Handle
	pid    uint32
	err    error
}

// Allocate the native callback once: Windows callback slots cannot be freed.
var closeWindowCallback = windows.NewCallback(func(hwnd uintptr, r *windowCloseRequest) uintptr {
	var pid uint32
	actionWindowPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == r.pid && processHandleAlive(r.handle) {
		if ok, _, e := actionPostMessage.Call(hwnd, 0x0010, 0, 0); ok == 0 {
			r.err = e
		}
	}
	return 1
})

func processHandleAlive(h windows.Handle) bool {
	var code uint32
	return windows.GetExitCodeProcess(h, &code) == nil && code == 259
}

// GracefulStop uses only the caller's already identity-checked process handle.
// ownedGroup must ONLY be true for a group created by StartChild and confined
// to its non-breakaway job. An observed orphan always supplies false.
func GracefulStop(h windows.Handle, pid uint32, ownedGroup bool) error {
	if !processHandleAlive(h) {
		return nil
	}
	if planWindowsGraceful(ownedGroup, sharedConsole(pid), ownedGroup) == consoleBreak {
		if err := windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, pid); err == nil {
			return nil
		}
	}
	r := windowCloseRequest{handle: h, pid: pid}
	if ok, _, err := actionEnumWindows.Call(closeWindowCallback, uintptr(unsafe.Pointer(&r))); ok == 0 {
		return err
	}
	return r.err // A process without windows still receives the full grace period.
}
