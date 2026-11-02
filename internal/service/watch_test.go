package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatchExecutableStopsWhenRemoved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "drigad")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	gone := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		WatchExecutable(t.Context(), path, time.Millisecond, func() { close(gone) })
	}()
	select {
	case <-gone:
		t.Fatal("gone before removal")
	case <-time.After(20 * time.Millisecond):
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	select {
	case <-gone:
	case <-time.After(5 * time.Second):
		t.Fatal("removal not noticed")
	}
	<-done
}

func TestWatchExecutableEndsWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		WatchExecutable(ctx, filepath.Join(t.TempDir(), "drigad"), time.Hour, func() { t.Error("gone called") })
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("watch outlived its context")
	}
}
