package tail

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/model"
)

func writeLog(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "log.jsonl")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func appendLog(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.WriteString(f, content)
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("append: %v, close: %v", err, closeErr)
	}
}

func wantLines(t *testing.T, r Result, want ...string) {
	t.Helper()
	got := make([]string, len(r.Lines))
	for i, line := range r.Lines {
		got[i] = string(line)
	}
	if !reflect.DeepEqual(got, want) && !(len(got) == 0 && len(want) == 0) {
		t.Fatalf("lines = %q, want %q", got, want)
	}
}

func mustRead(t *testing.T, path string, prev model.Offset, budget int64) Result {
	t.Helper()
	r, err := Read(path, prev, budget)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestReadCompleteLinesOnly(t *testing.T) {
	path := writeLog(t, "a\nb\npart")
	r := mustRead(t, path, model.Offset{}, 1024)
	wantLines(t, r, "a", "b")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if r.Next.Pos != 4 || r.Next.Inode == 0 || r.Next.Size != 8 || !r.Next.Mtime.Equal(info.ModTime()) {
		t.Fatalf("offset = %+v", r.Next)
	}
	appendLog(t, path, "ial\n")
	next := mustRead(t, path, r.Next, 1024)
	wantLines(t, next, "partial")
	if next.Reset || next.Next.Pos != 12 {
		t.Fatalf("result = %+v", next)
	}
	wantLines(t, r, "a", "b") // Later reads must not overwrite retained lines.
	r.Lines[0][0] = 'z'
	if string(r.Lines[1]) != "b" {
		t.Fatal("lines alias each other")
	}
	wantLines(t, mustRead(t, path, next.Next, 1024))
	large := strings.Repeat("x", 128<<10)
	path = writeLog(t, large+"\nsecond\n\n")
	wantLines(t, mustRead(t, path, model.Offset{}, 1<<20), large, "second", "")
}

func TestReadResetOnTruncate(t *testing.T) {
	path := writeLog(t, "long original line\n")
	r := mustRead(t, path, model.Offset{}, 1024)
	if err := os.WriteFile(path, []byte("new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	r = mustRead(t, path, r.Next, 1024)
	wantLines(t, r, "new")
	if !r.Reset || r.Next.Pos != 4 || r.Next.Size != 4 {
		t.Fatalf("result = %+v", r)
	}
}

func TestReadResetOnInodeChange(t *testing.T) {
	path := writeLog(t, "old\n")
	r := mustRead(t, path, model.Offset{}, 1024)
	oldInode := r.Next.Inode
	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement\n"), 0600); err != nil {
		t.Fatal(err)
	}
	r = mustRead(t, path, r.Next, 1024)
	wantLines(t, r, "replacement")
	if !r.Reset || r.Next.Inode == oldInode {
		t.Fatalf("result = %+v", r)
	}
}

func TestTailOversizeLine(t *testing.T) {
	path := writeLog(t, "first\n")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	chunk := bytes.Repeat([]byte("x"), 64<<10)
	for range 96 {
		if _, err := f.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := io.WriteString(f, "\nlast\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	r := mustRead(t, path, model.Offset{}, 16<<20)
	runtime.ReadMemStats(&after)
	wantLines(t, r, "first", "last")
	if r.Skipped != 1 || r.Next.Pos != int64(len("first\n")+(6<<20)+len("\nlast\n")) {
		t.Fatalf("result = %+v", r)
	}
	// TotalAlloc also catches transient buffers that a GC could hide in HeapAlloc.
	if delta := after.TotalAlloc - before.TotalAlloc; delta >= 2<<20 {
		t.Fatalf("heap allocation delta = %d, want < 2 MiB", delta)
	}
	if after.HeapAlloc > before.HeapAlloc && after.HeapAlloc-before.HeapAlloc >= 2<<20 {
		t.Fatalf("heap delta = %d, want < 2 MiB", after.HeapAlloc-before.HeapAlloc)
	}

	t.Run("partial", func(t *testing.T) {
		path := writeLog(t, "first\n"+strings.Repeat("x", MaxLine+1))
		r := mustRead(t, path, model.Offset{}, 16<<20)
		wantLines(t, r, "first")
		if r.Next.Pos != 6 || r.Skipped != 0 {
			t.Fatalf("partial oversize result = %+v", r)
		}
		appendLog(t, path, "\nlast\n")
		r = mustRead(t, path, r.Next, 16<<20)
		wantLines(t, r, "last")
		if r.Skipped != 1 {
			t.Fatalf("Skipped = %d", r.Skipped)
		}
	})
	t.Run("limit", func(t *testing.T) {
		line := strings.Repeat("x", MaxLine)
		path := writeLog(t, line+"\n"+line+"x\nok\n")
		r := mustRead(t, path, model.Offset{}, 16<<20)
		wantLines(t, r, line, "ok")
		if r.Skipped != 1 {
			t.Fatalf("Skipped = %d", r.Skipped)
		}
	})
}

func TestReadRespectsMaxBytes(t *testing.T) {
	path := writeLog(t, "aaa\nbbb\nccc\n")
	var prev model.Offset
	for _, line := range []string{"aaa", "bbb", "ccc"} {
		r := mustRead(t, path, prev, 6)
		wantLines(t, r, line)
		if r.Next.Pos-prev.Pos != 4 {
			t.Fatalf("consumed %d, want 4", r.Next.Pos-prev.Pos)
		}
		prev = r.Next
	}
	path = writeLog(t, "longer than budget\nnext\n")
	r := mustRead(t, path, model.Offset{}, 3)
	wantLines(t, r, "longer than budget")
	if r.Next.Pos != int64(len("longer than budget\n")) {
		t.Fatalf("offset = %+v", r.Next)
	}
	wantLines(t, mustRead(t, path, r.Next, 3), "next")
	for _, budget := range []int64{0, -1} {
		r := mustRead(t, path, model.Offset{}, budget)
		wantLines(t, r)
		if r.Next.Pos != 0 {
			t.Fatalf("budget %d advanced offset: %+v", budget, r.Next)
		}
	}
	t.Run("oversize-budget", func(t *testing.T) {
		path := writeLog(t, strings.Repeat("x", MaxLine+1)+"\nok\n")
		var prev model.Offset
		skipped := 0
		for range 6 {
			r := mustRead(t, path, prev, 1<<20)
			if delta := r.Next.Pos - prev.Pos; delta < 0 || delta > 1<<20 {
				t.Fatalf("oversize consumed %d bytes, budget = 1 MiB", delta)
			}
			skipped += r.Skipped
			prev = r.Next
			if len(r.Lines) > 0 {
				wantLines(t, r, "ok")
				if skipped != 1 {
					t.Fatalf("Skipped = %d, want 1", skipped)
				}
				return
			}
		}
		t.Fatal("oversize line did not make progress")
	})
}

func TestSeekEnd(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		pos           int64
	}{
		{"empty", "", 0}, {"complete", "a\nb\n", 4},
		{"partial", "a\nb\npart", 4}, {"only-partial", "part", 0},
		{"long-partial", "a\n" + strings.Repeat("x", 128<<10), 2},
		{"chunk-boundary", "a\n" + strings.Repeat("x", (64<<10)-1), 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeLog(t, tc.content)
			o, err := SeekEnd(path)
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if o.Pos != tc.pos || o.Size != int64(len(tc.content)) || o.Inode == 0 || !o.Mtime.Equal(info.ModTime()) {
				t.Fatalf("offset = %+v", o)
			}
			appendLog(t, path, "done\n")
			r := mustRead(t, path, o, 1<<20)
			wantLines(t, r, tc.content[tc.pos:]+"done")
			if r.Reset {
				t.Fatal("append reset baseline")
			}
		})
	}
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := SeekEnd(missing); !os.IsNotExist(err) {
		t.Fatalf("SeekEnd(missing) error = %v", err)
	}
	if _, err := Read(missing, model.Offset{}, 1024); !os.IsNotExist(err) {
		t.Fatalf("Read(missing) error = %v", err)
	}
}
