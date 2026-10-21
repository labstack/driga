package tail

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/model"
)

func TestSeekRecentLineBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, data  string
		budget, pos int64
	}{
		{"exact boundary", "old\nrecent\n", 7, 4},
		{"inside line", "old\nmiddle\nrecent\n", 10, 11},
		{"at newline", "old\nmiddle\nrecent\n", 8, 11},
		{"malformed line", "old\ninvalid {\"type\":\"event\"}\nrecent\n", 20, 29},
		{"long line", strings.Repeat("x", 128<<10) + "\nrecent\n", 1024, (128 << 10) + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeLog(t, tc.data)
			o, skipped, err := SeekRecent(path, model.Offset{}, tc.budget)
			if err != nil || o.Pos != tc.pos || skipped != tc.pos || o.Inode == 0 || o.Size != int64(len(tc.data)) {
				t.Fatalf("offset=%+v skipped=%d err=%v want pos=%d", o, skipped, err, tc.pos)
			}
			if tc.data[o.Pos-1] != '\n' {
				t.Fatalf("split line at %d", o.Pos)
			}
			r := mustRead(t, path, o, tc.budget)
			wantLines(t, r, "recent")
			if r.Reset || r.Next.Pos != int64(len(tc.data)) {
				t.Fatalf("read=%+v", r)
			}
		})
	}
}

func TestSeekRecentBacklogAndReset(t *testing.T) {
	path := writeLog(t, "old\nmiddle\nrecent\n")
	initial := mustRead(t, path, model.Offset{}, 4).Next
	for _, tc := range []struct {
		name        string
		prev        model.Offset
		wantSkipped int64
	}{
		{"existing offset", initial, 7},
		{"rotated", model.Offset{Inode: initial.Inode + 1, Pos: 4}, 11},
		{"truncated", model.Offset{Inode: initial.Inode, Pos: 100}, 11},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, skipped, err := SeekRecent(path, tc.prev, 10)
			if err != nil || o.Pos != 11 || skipped != tc.wantSkipped {
				t.Fatalf("offset=%+v skipped=%d err=%v", o, skipped, err)
			}
			wantLines(t, mustRead(t, path, o, 10), "recent")
		})
	}
	for _, budget := range []int64{0, -1, 100} {
		o, skipped, err := SeekRecent(path, initial, budget)
		if err != nil || o != initial || skipped != 0 {
			t.Fatalf("budget=%d offset=%+v skipped=%d err=%v", budget, o, skipped, err)
		}
	}
	missing := filepath.Join(t.TempDir(), "missing")
	if _, _, err := SeekRecent(missing, model.Offset{}, 10); !os.IsNotExist(err) {
		t.Fatalf("missing err=%v", err)
	}
}

func TestSeekRecentRetainsPartialLine(t *testing.T) {
	for _, prefix := range []string{"", "old\n"} {
		t.Run(prefix, func(t *testing.T) {
			path := writeLog(t, prefix+"partial")
			o, skipped, err := SeekRecent(path, model.Offset{}, 3)
			if err != nil || o.Pos != int64(len(prefix)) || skipped != int64(len(prefix)) {
				t.Fatalf("offset=%+v skipped=%d err=%v", o, skipped, err)
			}
			r := mustRead(t, path, o, 3)
			wantLines(t, r)
			if r.Next.Pos != int64(len(prefix)) {
				t.Fatalf("partial offset=%+v", r.Next)
			}
			appendLog(t, path, " done\n")
			wantLines(t, mustRead(t, path, r.Next, 100), "partial done")
		})
	}
}
