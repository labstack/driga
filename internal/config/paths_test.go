package config

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/variant"
)

func TestDataDirLinuxPaths(t *testing.T) {
	t.Cleanup(variant.SetForTest("")) // a release build
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
	// The default is the directory without the override.
	if got := defaultDataDirForOS("linux"); got != filepath.Join(home, "xdg/driga") {
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

// A development build keeps its own database and spool, beside a release's,
// and shares the configuration file.
func TestDevelopmentBuildDataDirs(t *testing.T) {
	t.Cleanup(variant.SetForTest("-dev"))
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, key := range []string{"DRIGA_DATA_DIR", "DRIGA_SPOOL_DIR", "DRIGA_CONFIG", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CONFIG_HOME"} {
		t.Setenv(key, "")
	}
	for _, tc := range []struct{ got, want string }{
		{dataDirForOS("darwin"), filepath.Join(home, "Library/Application Support/driga-dev")},
		{spoolDirForOS("darwin"), filepath.Join(home, "Library/Application Support/driga-dev/events")},
		{dataDirForOS("linux"), filepath.Join(home, ".local/share/driga-dev")},
		{spoolDirForOS("linux"), filepath.Join(home, ".local/state/driga-dev/events")},
		{configPath(), filepath.Join(home, ".config/driga/config.toml")},
	} {
		if runtime.GOOS == "windows" && tc.want == filepath.Join(home, ".config/driga/config.toml") {
			continue
		}
		if tc.got != tc.want {
			t.Errorf("got %s, want %s", tc.got, tc.want)
		}
	}
}

// StateDirs names every variant's state, whichever variant asks, plus this
// process's overrides.
func TestStateDirsCoverEveryVariant(t *testing.T) {
	for _, suffix := range []string{"", "-dev"} {
		t.Run("variant"+suffix, func(t *testing.T) {
			t.Cleanup(variant.SetForTest(suffix))
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			for _, key := range []string{"XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CONFIG_HOME"} {
				t.Setenv(key, "")
			}
			t.Setenv("DRIGA_DATA_DIR", filepath.Join(home, "data"))
			t.Setenv("DRIGA_SPOOL_DIR", filepath.Join(home, "spool"))
			t.Setenv("DRIGA_CONFIG", filepath.Join(home, "custom.toml"))
			if err := os.MkdirAll(filepath.Join(home, ".local", "share", "driga-feature"), 0o700); err != nil {
				t.Fatal(err)
			}
			got := stateDirsForOS("linux")
			for _, want := range []string{
				filepath.Join(home, ".local/share/driga"), filepath.Join(home, ".local/share/driga-dev"), filepath.Join(home, ".local/share/driga-feature"),
				filepath.Join(home, ".local/state/driga"), filepath.Join(home, ".local/state/driga-dev"),
				filepath.Join(home, ".config/driga"), filepath.Join(home, "data"), filepath.Join(home, "spool"), filepath.Join(home, "custom.toml"),
			} {
				if runtime.GOOS == "windows" && strings.Contains(want, ".config") {
					continue
				}
				if !slices.Contains(got, want) {
					t.Errorf("StateDirs lacks %s: %v", want, got)
				}
			}
		})
	}
}
