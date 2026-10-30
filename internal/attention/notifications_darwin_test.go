//go:build darwin

package attention

import (
	"testing"
	"time"

	"github.com/ebitengine/purego/objc"
)

// Exercises the completion-handler ABI without loading UserNotifications,
// requesting authorization, or posting any real desktop notification.
func TestNativeCompletionBlockABI(t *testing.T) {
	completed := false
	done := objc.NewBlock(func(_ objc.Block) { completed = true })
	defer done.Release()
	completeNativeBlock(done)
	if !completed {
		t.Fatal("completion not invoked")
	}
	var options uintptr
	present := objc.NewBlock(func(_ objc.Block, value uintptr) { options = value })
	defer present.Release()
	presentNativeBlock(present, 26)
	if options != 26 {
		t.Fatal(options)
	}
}

func TestServiceActivationPolicy(t *testing.T) {
	if serviceActivationPolicy != 2 {
		t.Fatal("service must be background-only (Prohibited)")
	}
}

func TestDispatchMainAndWait(t *testing.T) {
	t.Run("main thread runs inline", func(t *testing.T) {
		ran := false
		dispatchMainAndWait(true, func(func()) { t.Fatal("dispatching on main would deadlock before run loop starts") }, func() { ran = true })
		if !ran {
			t.Fatal("not run synchronously")
		}
	})
	t.Run("worker waits for main queue", func(t *testing.T) {
		queued := make(chan func(), 1)
		done := make(chan struct{})
		ran := false
		go func() {
			dispatchMainAndWait(false, func(fn func()) { queued <- fn }, func() { ran = true })
			close(done)
		}()
		var fn func()
		select {
		case fn = <-queued:
		case <-time.After(time.Second):
			t.Fatal("not queued")
		}
		select {
		case <-done:
			t.Fatal("returned before main queue executed")
		default:
		}
		fn()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("did not return")
		}
		if !ran {
			t.Fatal("callback not run")
		}
	})
}
