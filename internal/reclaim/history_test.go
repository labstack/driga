package reclaim

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/driga/internal/logfile"
)

func TestAppendReadAndDiskHistory(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	if err := Append(dir, Entry{At: at, ID: "tree-1", Kind: "process", Status: "partial", Signaled: []int32{1, 2}, Stopped: []int32{1}, Survivors: []int32{2}}); err != nil {
		t.Fatal(err)
	}
	disk := `{"occurred_at":"2026-11-09T12:01:00Z","candidate":{"id":"cache:uv","kind":"cache","path":"SECRET"},"result":{"freed_bytes":123,"commands":[["SECRET"]]},"error":""}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "cleanup.log"), []byte(disk), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := Read(dir, 2, "")
	if err != nil || len(got.Entries) != 2 || got.Entries[0].FreedBytes != 123 || got.Entries[1].Status != "partial" {
		t.Fatalf("%+v %v", got, err)
	}
	got, err = Read(dir, 1, "process")
	if err != nil || len(got.Entries) != 1 || len(got.Entries[0].Survivors) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestConcurrentReceiptsAndDamagedTail(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := Append(dir, Entry{Kind: "process", Status: "completed"}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	f, err := os.OpenFile(filepath.Join(dir, "reclaim.log"), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("null\n{damaged")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := Append(dir, Entry{Kind: "process", ID: "after-crash", Status: "completed"}); err != nil {
		t.Fatal(err)
	}
	got, err := Read(dir, 100, "")
	if err != nil || len(got.Entries) != 21 || len(got.Warnings) != 1 || got.Entries[0].ID != "after-crash" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestOversizeReceiptRefused(t *testing.T) {
	if err := Append(t.TempDir(), Entry{ID: strings.Repeat("x", 1<<20)}); err == nil {
		t.Fatal("unbounded receipt accepted")
	}
}

func TestRotationRetainsPriorReceiptsWithinBound(t *testing.T) {
	dir := t.TempDir()
	old, err := json.Marshal(Entry{At: time.Now().Add(-time.Minute), Kind: "process", ID: "older", Status: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	padding := bytes.Repeat([]byte{' '}, int(logfile.MaxSize)-len(old)-2)
	data := append(append(append(padding, '\n'), old...), '\n')
	if err := os.WriteFile(filepath.Join(dir, "reclaim.log"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := Append(dir, Entry{Kind: "process", ID: "newer", Status: "completed"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"reclaim.log", "reclaim.log.1"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.Size() > logfile.MaxSize {
			t.Fatalf("%s info=%v err=%v", name, info, err)
		}
	}
	h, err := Read(dir, 2, "process")
	if err != nil || len(h.Entries) != 2 || h.Entries[0].ID != "newer" || h.Entries[1].ID != "older" {
		t.Fatalf("%+v %v", h, err)
	}
}
