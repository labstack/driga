//go:build !windows

package platform

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestOpenFilePathsSeesOwnFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "open")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	paths, err := New().(OpenFileLister).OpenFilePaths(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(paths, canonical) {
		t.Fatalf("own open file missing: %s", canonical)
	}
}
