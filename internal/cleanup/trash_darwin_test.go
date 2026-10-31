package cleanup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// macOS refuses to open ~/.Trash to a process without Full Disk Access, as
// the Driga service is, but lets it rename into it. A trash directory that
// cannot be read stands in for that here: scratch still moves into it, and an
// item already there keeps its name.
func TestScratchTrashUnreadable(t *testing.T) {
	e, _, _, _ := fixture(t)
	trash := filepath.Join(e.Home, ".Trash")
	put(t, filepath.Join(trash, "unreadable", "kept"), "keep")
	if err := os.Chmod(trash, 0o300); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(trash, 0o700) })
	if _, err := os.ReadDir(trash); err == nil {
		t.Skip("trash still readable; running as root?")
	}
	path := scratch(t, e, "unreadable")
	r, err := Apply(context.Background(), e, listed(t, e, path), ApplyOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("scratch still in place", err)
	}
	if filepath.Dir(r.TrashPath) != trash || filepath.Base(r.TrashPath) == "unreadable" {
		t.Fatal(r.TrashPath)
	}
	if _, err := os.Stat(filepath.Join(r.TrashPath, "file")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(trash, "unreadable", "kept")); err != nil {
		t.Fatal("existing trash item replaced", err)
	}
}
