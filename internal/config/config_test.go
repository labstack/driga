package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/labstack/driga/internal/config"
)

func TestDefaults(t *testing.T) {
	c := config.Default()
	if c.RootDepth != 3 || c.StuckAfter != 10*time.Minute || c.OrphanMinAge != 10*time.Minute {
		t.Fatal(c)
	}
	if c.CaptureText != "titles" || len(c.Roots) != 0 {
		t.Fatal(c)
	}
	if !c.Adapters["claude"].Enabled || !c.Adapters["codex"].Enabled {
		t.Fatal(c)
	}
	if diff := cmp.Diff([]string{"**/node_modules", "**/.tmp/**"}, c.Exclude); diff != "" {
		t.Fatal(diff)
	}
	if c.NoServiceHint {
		t.Fatal("no_service_hint must default false")
	}
}

func TestLoadMissingFileIsDefault(t *testing.T) {
	c, err := config.Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(config.Default(), c); diff != "" {
		t.Fatal(diff)
	}
}

func TestLoadOverrides(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	p := filepath.Join(t.TempDir(), "c.toml")
	body := "stuck_after = \"3m\"\nroots = [\"~/code\"]\ncapture_text = \"snippets\"\n[adapters.codex]\nenabled = false\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.StuckAfter != 3*time.Minute || c.CaptureText != "snippets" {
		t.Fatal(c)
	}
	if diff := cmp.Diff([]string{filepath.Join(home, "code")}, c.Roots); diff != "" {
		t.Fatal(diff)
	}
	if c.Adapters["codex"].Enabled || !c.Adapters["claude"].Enabled {
		t.Fatal(c.Adapters)
	}
	if c.OrphanMinAge != 10*time.Minute {
		t.Fatal("unset keys must keep defaults")
	}
}

func TestLoadRejectsBadCaptureText(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	if err := os.WriteFile(p, []byte(`capture_text = "all"`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(p)
	if err == nil || !strings.Contains(err.Error(), "capture_text") {
		t.Fatalf("want capture_text error, got %v", err)
	}
}

func TestPathsHonorEnv(t *testing.T) {
	t.Setenv("DRIGA_DATA_DIR", "/x/data")
	t.Setenv("DRIGA_SPOOL_DIR", "/x/spool")
	t.Setenv("DRIGA_CONFIG", "/x/c.toml")
	if config.DataDir() != "/x/data" || config.SpoolDir() != "/x/spool" || config.Path() != "/x/c.toml" {
		t.Fatal(config.DataDir(), config.SpoolDir(), config.Path())
	}
	t.Setenv("DRIGA_DATA_DIR", "")
	t.Setenv("DRIGA_SPOOL_DIR", "")
	t.Setenv("DRIGA_CONFIG", "")
	t.Setenv("HOME", "/h")
	if config.DataDir() != "/h/Library/Application Support/driga" || config.SpoolDir() != "/h/.driga/events" || config.Path() != "/h/.config/driga/config.toml" {
		t.Fatal(config.DataDir(), config.SpoolDir(), config.Path())
	}
}
