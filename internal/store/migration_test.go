package store

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestReopenBaselineSkipsParser(t *testing.T) {
	// goose's SQL parser pools 4 MiB scan buffers; a collector restart on an
	// up-to-date database must not pay that spike before its first pass.
	path := filepath.Join(t.TempDir(), "driga.db")
	st, err := Open(path, ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	var before, after runtime.MemStats
	// Two collections empty sync.Pool and its victim cache, as in a new process.
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&before)
	st, err = Open(path, ReadWrite)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	if got := after.TotalAlloc - before.TotalAlloc; got >= 4<<20 {
		t.Fatalf("reopen allocated %d bytes, want < 4 MiB", got)
	}
}
