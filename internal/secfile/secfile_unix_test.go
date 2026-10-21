//go:build !windows

package secfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnixPrivateModes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	if err := MkdirPrivate(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "key")
	if err := WritePrivate(path, []byte("key")); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]os.FileMode{dir: 0700, path: 0600} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Fatalf("mode %o, want %o", info.Mode().Perm(), want)
		}
	}
	if err := CheckPrivate(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if err := CheckPrivate(path); err == nil {
		t.Fatal("public key accepted")
	}
	// Existing directory modes are preserved unless explicitly protected.
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := MkdirPrivate(dir); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0755 {
		t.Fatal("existing directory mode changed")
	}
	if err := Protect(dir); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(dir)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatal(info, err)
	}
}
