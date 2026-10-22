package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDataDirLinuxPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
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

func TestRelativePathsAreAbsolute(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, key := range []string{"DRIGA_DATA_DIR", "DRIGA_SPOOL_DIR", "DRIGA_CONFIG"} {
		t.Setenv(key, "a/../b/space #?%")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(wd, "b", "space #?%")
	for _, p := range []string{DataDir(), SpoolDir(), Path()} {
		if p != want {
			t.Fatalf("got %q, want %q", p, want)
		}
	}
}

func TestStartupPathsStayFixedAcrossChdir(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	for _, key := range []string{"DRIGA_DATA_DIR", "DRIGA_SPOOL_DIR", "DRIGA_CONFIG"} {
		t.Setenv(key, "a/../b")
	}
	if err := ResolvePaths(); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir("other", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir("other"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{DataDir(), SpoolDir(), Path()} {
		if p != filepath.Join(root, "b") {
			t.Fatalf("startup path moved: %s", p)
		}
	}
}
