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

// The desktop app runs on the main thread, and so does a collector posting its
// own notifications through AppKit. Other commands and collectors forwarding
// to a parent drop this package's lock on it.
func notificationMain(args []string, run func()) {
	if appMode(args) {
		run()
		return
	}
	cmd := command(args)
	appKit := usesAppKit(args)
	if appKit || cmd == "doctor" {
		// macOS identifies an app by the path it was started from. Through a
		// symlink such as /opt/homebrew/bin/driga it would not be Driga.app,
		// so restart from the real path for the commands that use that identity.
		if exe, err := os.Executable(); err == nil {
			if real, err := filepath.EvalSymlinks(exe); err == nil && real != exe {
				// The original arguments: started as drigad, the program adds
				// "service run" itself, and would add it twice.
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
