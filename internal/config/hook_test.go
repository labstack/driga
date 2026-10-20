package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/labstack/driga/internal/config"
)

func TestHookCapturePolicyOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DRIGA_DATA_DIR", filepath.Join(home, "data"))
	t.Setenv("DRIGA_SPOOL_DIR", filepath.Join(home, "spool"))
	path := filepath.Join(home, "config.toml")
	t.Setenv("DRIGA_CONFIG", path)
	t.Setenv("DRIGA_CAPTURE_TEXT", "titles")
	if err := os.WriteFile(path, []byte("broken configuration"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := config.HookCaptureSnippets(); err != nil || got {
		t.Fatal(got, err)
	}
	t.Setenv("DRIGA_CAPTURE_TEXT", "snippets")
	if got, err := config.HookCaptureSnippets(); err != nil || !got {
		t.Fatal(got, err)
	}
	if err := os.Unsetenv("DRIGA_CAPTURE_TEXT"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("capture_text = 'snippets'\nroot_depth = 'unrelated invalid setting'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := config.HookCaptureSnippets(); err != nil || !got {
		t.Fatal(got, err)
	}
	if err := os.WriteFile(path, []byte("capture_text = 'none'"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := config.HookCaptureSnippets(); err == nil || got {
		t.Fatal(got, err)
	}
}
