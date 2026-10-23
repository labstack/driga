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

func TestStartupConfigDefaultsRemainOptional(t *testing.T) {
	root := t.TempDir()
	for key, value := range map[string]string{
		"HOME": root, "USERPROFILE": root,
		"APPDATA":         filepath.Join(root, "AppData", "Roaming"),
		"XDG_CONFIG_HOME": filepath.Join(root, ".config"),
		"DRIGA_CONFIG":    "", "DRIGA_DATA_DIR": "", "DRIGA_SPOOL_DIR": "",
	} {
		t.Setenv(key, value)
	}
	if err := ResolvePaths(); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("DRIGA_CONFIG"); got != "" {
		t.Fatalf("default config became an explicit override: %q", got)
	}
	if _, err := Load(Path()); err != nil {
		t.Fatalf("startup with missing default config: %v", err)
	}

	t.Setenv("DRIGA_CONFIG", "missing.toml")
	if err := ResolvePaths(); err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(os.Getenv("DRIGA_CONFIG")) {
		t.Fatal("explicit config path was not resolved")
	}
	if _, err := Load(Path()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("startup with missing explicit config: %v", err)
	}
}
