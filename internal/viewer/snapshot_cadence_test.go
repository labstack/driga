package viewer

import (
	"testing"
	"time"
)

func TestSnapshotIdleCadence(t *testing.T) {
	s := &LiveSnapshotSource{}
	now := time.Now()
	s.setActiveAt(false, now)
	if got := s.collectionInterval(now.Add(59 * time.Second)); got != 5*time.Second {
		t.Fatal(got)
	}
	if got := s.collectionInterval(now.Add(61 * time.Second)); got != time.Minute {
		t.Fatal(got)
	}
	s.setActiveAt(true, now.Add(62*time.Second))
	if got := s.collectionInterval(now.Add(63 * time.Second)); got != 5*time.Second {
		t.Fatal(got)
	}
}
