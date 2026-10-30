//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/labstack/driga/internal/attention"
	"github.com/labstack/driga/internal/service"
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
			real := exe
			if resolved, err := filepath.EvalSymlinks(exe); err == nil {
				real = resolved
			}
			if cmd == "doctor" {
				real = doctorExecutable(real)
			}
			if real != exe {
				// Preserve argv[0]: doctor still runs as the CLI, while Cocoa
				// queries permission using the service helper's bundle identity.
				_ = reexecResolved(real, os.Args, syscall.Exec)
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

// reexecResolved changes the executable path while preserving its invocation.
func reexecResolved(real string, argv []string, exec func(string, []string, []string) error) error {
	return exec(real, argv, os.Environ())
}

// doctorExecutable gives notification diagnostics the identity that posts alerts.
// Keeping argv[0] unchanged prevents drigad's ordinary service startup dispatch.
func doctorExecutable(exe string) string {
	if service.BundlePath(exe) != "" {
		return service.DaemonPath(exe)
	}
	return exe
}
