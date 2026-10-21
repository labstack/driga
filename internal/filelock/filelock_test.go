package filelock

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLockLifetime(t *testing.T) {
	p := filepath.Join(t.TempDir(), "lock")
	a, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := os.OpenFile(p, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err := Lock(a, true); err != nil {
		t.Fatal(err)
	}
	if err := Lock(b, true); !Busy(err) {
		t.Fatalf("second lock: %v", err)
	}
	if _, err := a.WriteAt([]byte("owner"), 0); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 5)
	if _, err := b.ReadAt(buf, 0); err != nil {
		t.Fatal(err)
	}
	if err := Unlock(a); err != nil {
		t.Fatal(err)
	}
	if err := Lock(b, false); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if err := Lock(a, true); err != nil {
		t.Fatal("close did not release:", err)
	}
}
