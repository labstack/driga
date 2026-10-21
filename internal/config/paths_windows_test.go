package config

import (
	"path/filepath"
	"testing"
)

func TestWindowsDefaultPathsAndOverrides(t *testing.T) {
	root := t.TempDir()
	for key, value := range map[string]string{"HOME": filepath.Join(root, "ignored"), "USERPROFILE": root, "APPDATA": filepath.Join(root, "roaming"), "LOCALAPPDATA": filepath.Join(root, "local"), "DRIGA_CONFIG": "", "DRIGA_DATA_DIR": "", "DRIGA_SPOOL_DIR": "", "XDG_CONFIG_HOME": filepath.Join(root, "xdg"), "XDG_STATE_HOME": filepath.Join(root, "state")} {
		t.Setenv(key, value)
	}
	if got := ExpandHome(`~\.claude`); got != filepath.Join(root, ".claude") {
		t.Fatal(got)
	}
	for _, tc := range []struct{ got, want string }{
		{Path(), filepath.Join(root, "roaming", "driga", "config.toml")},
		{DataDir(), filepath.Join(root, "local", "driga")},
		{SpoolDir(), filepath.Join(root, "local", "driga", "events")},
	} {
		if tc.got != tc.want {
			t.Fatalf("%q != %q", tc.got, tc.want)
		}
	}
	t.Setenv("DRIGA_CONFIG", filepath.Join(root, "custom.toml"))
	t.Setenv("DRIGA_DATA_DIR", filepath.Join(root, "data"))
	t.Setenv("DRIGA_SPOOL_DIR", filepath.Join(root, "spool"))
	if Path() != filepath.Join(root, "custom.toml") || DataDir() != filepath.Join(root, "data") || SpoolDir() != filepath.Join(root, "spool") {
		t.Fatal("overrides ignored")
	}
}
