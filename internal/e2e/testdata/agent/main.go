//go:build windows

// This synthetic executable never invokes an agent CLI. Its image name and
// arguments exercise the shipped signatures, while modes control only our tree.
package main

import (
	"encoding/json"
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"strconv"
	"time"
)

func main() {
	switch os.Getenv("DRIGA_E2E_MODE") {
	case "stop-console":
		pid, err := strconv.ParseUint(os.Getenv("DRIGA_E2E_PID"), 10, 32)
		if err != nil {
			panic(err)
		}
		kernel := windows.NewLazySystemDLL("kernel32.dll")
		kernel.NewProc("FreeConsole").Call()
		if ok, _, err := kernel.NewProc("AttachConsole").Call(uintptr(pid)); ok == 0 {
			panic(err)
		}
		defer kernel.NewProc("FreeConsole").Call()
		if err := windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(pid)); err != nil {
			panic(err)
		}
		time.Sleep(100 * time.Millisecond)
		return
	case "supervisor":
		spawn("root")
		return
	case "root":
		leaf := spawn("leaf")
		b, err := json.Marshal([]int{os.Getpid(), leaf.Process.Pid})
		if err != nil {
			panic(err)
		}
		if err := os.WriteFile(os.Getenv("DRIGA_E2E_HANDSHAKE"), b, 0600); err != nil {
			panic(err)
		}
	}
	time.Sleep(time.Hour)
}

func spawn(mode string) *exec.Cmd {
	child := exec.Command(os.Args[0], "mcp", "serve")
	child.Env = append(os.Environ(), "DRIGA_E2E_MODE="+mode)
	child.Dir, _ = os.Getwd()
	if err := child.Start(); err != nil {
		panic(err)
	}
	return child
}
