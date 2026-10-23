package chat

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/labstack/driga/internal/config"
)

func TestMCPConfigRemainsOptional(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DRIGA_CONFIG", "")
	t.Setenv("DRIGA_DATA_DIR", filepath.Join(root, "data"))
	t.Setenv("DRIGA_SPOOL_DIR", filepath.Join(root, "spool"))
	if err := config.ResolvePaths(); err != nil {
		t.Fatal(err)
	}
	env, err := mcpEnvironment(config.DataDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, explicit := env["DRIGA_CONFIG"]; explicit {
		t.Fatal("MCP child requires the optional default config")
	}
	if env["DRIGA_DATA_DIR"] != config.DataDir() || env["DRIGA_SPOOL_DIR"] != config.SpoolDir() {
		t.Fatal("MCP child lost its storage paths", env)
	}
	// An explicit override must still reach the child as an absolute path,
	// even when absent, so it retains the normal missing-file error.
	t.Setenv("DRIGA_CONFIG", "missing config.toml")
	env, err = mcpEnvironment(config.DataDir())
	if err != nil || env["DRIGA_CONFIG"] != config.Path() || !filepath.IsAbs(env["DRIGA_CONFIG"]) {
		t.Fatalf("explicit override was not preserved: %v %v", env, err)
	}
	if _, err := os.Stat(env["DRIGA_CONFIG"]); !os.IsNotExist(err) {
		t.Fatalf("test override unexpectedly exists: %v", err)
	}
}
