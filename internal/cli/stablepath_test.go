package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/inbox"
)

func TestNotifierUsesStablePath(t *testing.T) {
	readEnv(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "driga")
	if err := os.Symlink(exe, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(link))
	d, err := NewDeps(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	n := d.Notifier.(inbox.DesktopNotifier)
	if n.DrigaPath != link {
		t.Fatalf("notifier path = %q, want %q", n.DrigaPath, link)
	}
}

// Homebrew links bin/driga to Cellar/driga/<version>/bin/driga. The service and
// hooks must record the link, or brew upgrade leaves them on a deleted file.
func TestStablePathPrefersPackageManagerLink(t *testing.T) {
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
