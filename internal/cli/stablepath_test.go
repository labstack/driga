package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Homebrew links bin/driga to Cellar/driga/<version>/bin/driga. The service and
// hooks must record the link, or brew upgrade leaves them on a deleted file.
func TestStablePathPrefersPackageManagerLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix symlink fixture; Windows watch tests require no Developer Mode or admin rights")
	}

	root := t.TempDir()
	cellar := filepath.Join(root, "Cellar", "driga", "1.8.0", "bin", "driga")
	link := filepath.Join(root, "bin", "driga")
	other := filepath.Join(root, "other", "driga")
	for _, p := range []string{cellar, other} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(cellar, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(link))
	if got := stablePath(cellar); got != link {
		t.Fatalf("stablePath(cellar) = %q, want %q", got, link)
	}
	if got := stablePath(other); got != other {
		t.Fatalf("a different driga on PATH must not replace this one: %q", got)
	}
	t.Setenv("PATH", "")
	if got := stablePath(cellar); got != cellar {
		t.Fatalf("no driga on PATH: %q", got)
	}
}
