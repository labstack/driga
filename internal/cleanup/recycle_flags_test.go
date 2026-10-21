package cleanup

import "testing"

func TestRecycleFlags(t *testing.T) {
	const required = 0x20180454 // recycle, silent, no confirmation/error UI, undo, early failure
	if got := recycleFlags(); got != required {
		t.Fatalf("flags %#x: require recycling, undo and early failure", got)
	}
}
