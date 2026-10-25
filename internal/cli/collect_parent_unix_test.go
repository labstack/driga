//go:build !windows

package cli

import (
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
)

// A running collector whose parent goes away, closing the write end of its
// lifeline pipe, exits cleanly instead of running on as an orphan.
func TestCollectExitsWhenParentGone(t *testing.T) {
	parentR, parentW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer parentW.Close()
	notifyR, notifyW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer notifyR.Close()
	// Hand the collector its own descriptors, as a child process has: it
	// closes them when it exits.
	parentFD := dup(t, parentR)
	notifyFD := dup(t, notifyW)
	serve := false
	done := make(chan error, 1)
	go func() {
		done <- CollectCmd{Owner: "app", ParentFD: &parentFD, NotifyFD: &notifyFD, Serve: &serve}.Run(nil)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		owner, held, err := collector.ReadOwner(config.DataDir())
		if err != nil {
			t.Fatal(err)
		}
		if held && owner.Kind == "app" {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("collector exited before the parent did: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("collector never took the lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	parentW.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("collector kept running without its parent")
	}
}

func dup(t *testing.T, f *os.File) int {
	t.Helper()
	fd, err := syscall.Dup(int(f.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return fd
}
