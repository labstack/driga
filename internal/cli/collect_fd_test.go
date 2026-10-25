package cli

import (
	"os"
	"strings"
	"testing"
)

// closedFD is a descriptor number that was open and is now closed.
func closedFD(t *testing.T) int {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	fd := int(r.Fd())
	r.Close()
	w.Close()
	return fd
}

// A descriptor the parent did not pass fails at once, before the collector
// runs with a notification pipe or lifeline that is not there.
func TestCollectRejectsClosedDescriptors(t *testing.T) {
	notify := closedFD(t)
	err := CollectCmd{NotifyFD: &notify}.Run(nil)
	if err == nil || !strings.Contains(err.Error(), "--notify-fd is not open") {
		t.Fatalf("closed notify fd: %v", err)
	}
	parent := closedFD(t)
	err = CollectCmd{ParentFD: &parent}.Run(nil)
	if err == nil || !strings.Contains(err.Error(), "--parent-fd is not open") {
		t.Fatalf("closed parent fd: %v", err)
	}
}
