package collector

import (
	"bytes"
	"github.com/labstack/driga/internal/logfile"
	"os"
	"path/filepath"
	"testing"
)

func TestCollectorLogRotates(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "collector.log")
	w := &logfile.Writer{Path: path}
	chunk := bytes.Repeat([]byte("a"), 1<<20)
	for i := 0; i < 25; i++ {
		if n, err := w.Write(chunk); err != nil || n != len(chunk) {
			t.Fatal(n, err)
		}
	}
	for _, p := range []string{path, path + ".1"} {
		info, err := os.Stat(p)
		if err != nil || info.Size() > logfile.MaxSize {
			t.Fatal(info, err)
		}
	}
	if _, err := os.Stat(path + ".2"); !os.IsNotExist(err) {
		t.Fatal("more than one rotated log", err)
	}
	if _, err := w.Write(bytes.Repeat([]byte("b"), 12<<20)); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() > logfile.MaxSize {
		t.Fatal(info, err)
	}
}
