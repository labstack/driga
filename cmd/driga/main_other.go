//go:build !darwin

package main

// The UI toolkit's package keeps the main goroutine on the main thread, where
// the desktop app runs.
func notificationMain(_ []string, run func()) { run() }
