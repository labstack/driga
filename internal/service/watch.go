package service

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"
)

// ExecutableCheck is how often the macOS service checks that its executable
// still exists.
const ExecutableCheck = time.Minute

// WatchExecutable calls gone once path no longer exists, then returns; it
// returns without calling gone when ctx ends. The macOS service cancels its
// own run with it after Driga.app is deleted without uninstalling the
// service, and launchd, which keeps it alive only while that path exists,
// does not start it again.
func WatchExecutable(ctx context.Context, path string, every time.Duration, gone func()) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			slog.Info("the Driga service's executable was removed; stopping", "event", "service_executable_removed", "path", path)
			gone()
			return
		}
	}
}
