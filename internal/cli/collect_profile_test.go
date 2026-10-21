package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCPUProfileStopsAfterDuration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cpu.pprof")
	stop, err := startCPUProfile(path, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if fi, err := os.Stat(path); err == nil && fi.Size() > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("profile not written after its duration")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for {
		if fi, err := os.Stat(path + ".alloc"); err == nil && fi.Size() > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("allocation profile not written after its duration")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// A second profile can start, so the first one released the profiler.
	again, err := startCPUProfile(filepath.Join(t.TempDir(), "again.pprof"), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	again()
}

func TestCPUProfileStopFlushesEarly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cpu.pprof")
	stop, err := startCPUProfile(path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	stop()
	stop()
	if fi, err := os.Stat(path); err != nil || fi.Size() == 0 {
		t.Fatalf("profile not flushed on stop: %v", err)
	}
	if fi, err := os.Stat(path + ".alloc"); err != nil || fi.Size() == 0 {
		t.Fatalf("allocation profile not flushed on stop: %v", err)
	}
}
