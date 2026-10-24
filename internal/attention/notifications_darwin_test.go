//go:build darwin

package attention

import (
	"testing"

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
