//go:build darwin

package main

import (
	"os"
	"runtime"
	"strings"

	"github.com/labstack/driga/internal/attention"
)

// Cocoa's application/run loop must own the initial OS thread. Lock in init,
// before Go can move main to a worker thread. CLI work runs on a Go goroutine.
func init() { runtime.LockOSThread() }

// Only the collector posts notifications. Other commands, including hooks
// that run on every agent event, skip AppKit and release the thread.
func notificationMain(run func()) {
	if command(os.Args[1:]) != "collect" {
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
