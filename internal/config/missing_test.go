package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMissingExplicitConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("DRIGA_CONFIG", "")
	if _, err := Load(Path()); err != nil {
		t.Fatal("missing default", err)
	}
	t.Setenv("DRIGA_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	if _, err := Load(Path()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing explicit config: %v", err)
	}
}
