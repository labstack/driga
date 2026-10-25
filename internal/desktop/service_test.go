package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

// The service manager knows one service per account, so an app run with
// another HOME must leave it alone; the same home through a symlink is the
// same home.
func TestIsolatedHome(t *testing.T) {
	if !isolatedHome(func() (string, string) { return "/tmp/fake-home", "/Users/someone" }) {
		t.Fatal("another HOME is not isolated")
	}
	if isolatedHome(func() (string, string) { return "", "/Users/someone" }) {
		t.Fatal("an unset HOME is isolated")
	}
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "home")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("no symlinks:", err)
	}
	if isolatedHome(func() (string, string) { return link, real }) {
		t.Fatal("a symlinked home is isolated")
	}
}
