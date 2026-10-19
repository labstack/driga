package config

import (
	"path/filepath"
	"testing"
)

func TestDataDirLinuxPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DRIGA_DATA_DIR", "")
	t.Setenv("XDG_DATA_HOME", "")
	if got := dataDirForOS("linux"); got != filepath.Join(home, ".local/share/driga") {
		t.Fatal(got)
	}
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "xdg"))
	if got := dataDirForOS("linux"); got != filepath.Join(home, "xdg/driga") {
		t.Fatal(got)
	}
	if got := dataDirForOS("darwin"); got != filepath.Join(home, "Library/Application Support/driga") {
		t.Fatal(got)
	}
	t.Setenv("DRIGA_DATA_DIR", filepath.Join(home, "override"))
	if got := dataDirForOS("linux"); got != filepath.Join(home, "override") {
		t.Fatal(got)
	}
}
