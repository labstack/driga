//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/labstack/driga/internal/attention"
)

// Cocoa's application/run loop must own the initial OS thread. Lock in init,
// before Go can move main to a worker thread. CLI work runs on a Go goroutine.
func init() { runtime.LockOSThread() }

// Only a collector posting its own notifications starts AppKit. Other commands
// and collectors forwarding to a parent release the thread.
func notificationMain(run func()) {
	cmd := command(os.Args[1:])
	appKit := usesAppKit(os.Args[1:])
	if appKit || cmd == "doctor" {
		// macOS identifies an app by the path it was started from. Through a
		// symlink such as /opt/homebrew/bin/driga it would not be Driga.app,
		// so restart from the real path for the commands that use that identity.
		if exe, err := os.Executable(); err == nil {
			if real, err := filepath.EvalSymlinks(exe); err == nil && real != exe {
				_ = syscall.Exec(real, append([]string{real}, os.Args[1:]...), os.Environ())
			}
		}
	}
	if !appKit {
		runtime.UnlockOSThread()
		run()
		return
	}
	attention.RunNotificationMain(run)
}
