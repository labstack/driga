//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/labstack/driga/internal/attention"
)

// Cocoa's application/run loop must own the initial OS thread. Lock in init,
// before Go can move main to a worker thread. CLI work runs on a Go goroutine.
func init() { runtime.LockOSThread() }

// Only the collector posts notifications. Other commands, including hooks
// that run on every agent event, skip AppKit and release the thread.
func notificationMain(run func()) {
	cmd := command(os.Args[1:])
	if cmd == "collect" || cmd == "doctor" {
		// macOS identifies an app by the path it was started from. Through a
		// symlink such as /opt/homebrew/bin/driga it would not be Driga.app,
		// so restart from the real path for the commands that use that identity.
		if exe, err := os.Executable(); err == nil {
			if real, err := filepath.EvalSymlinks(exe); err == nil && real != exe {
				_ = syscall.Exec(real, append([]string{real}, os.Args[1:]...), os.Environ())
			}
		}
	}
	if cmd != "collect" {
		runtime.UnlockOSThread()
		run()
		return
	}
	attention.RunNotificationMain(run)
}

func command(args []string) string {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}
