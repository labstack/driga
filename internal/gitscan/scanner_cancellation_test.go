package gitscan

import (
	"context"
	"fmt"
	"testing"
)

func TestCancelledScanRetainsNoLocks(t *testing.T) {
	s := NewScanner(1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for i := 0; i < 1000; i++ {
		if _, err := s.Scan(ctx, fmt.Sprintf("cancelled/%d", i), Full); err != context.Canceled {
			t.Fatal(err)
		}
	}
	if len(s.locks) != 0 || len(s.repos) != 0 || len(s.cache) != 0 {
		t.Fatal("cancelled scan retained state")
	}
}
