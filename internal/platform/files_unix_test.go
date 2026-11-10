//go:build !windows

package platform

import (
	"context"
	"errors"
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
	if errors.Is(err, os.ErrPermission) {
		// A host may contain unreadable same-user descriptors. The production
		// scan correctly refuses a partial inventory; this positive integration
		// assertion requires a host that permits a complete observation.
		t.Skipf("host prevents complete open-file inspection: %v", err)
	}
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
