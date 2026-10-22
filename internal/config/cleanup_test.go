package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCleanupConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[cleanup]\nworktree_idle = '48h'\nlow_disk_bytes = '2GiB'\nregenerable = ['node_modules/', 'cache/generated/']\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cleanup.WorktreeIdle != 48*time.Hour || cfg.Cleanup.LowDiskBytes != 2<<30 || len(cfg.Cleanup.Regenerable) != len(Default().Cleanup.Regenerable)+1 {
		t.Fatal(cfg.Cleanup)
	}
	for _, line := range []string{"worktree_idle = '-1h'", "low_disk_bytes = '999999999999999999999GiB'", "regenerable = ['../outside/']"} {
		if err := os.WriteFile(path, []byte("[cleanup]\n"+line), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatal("accepted", line)
		}
	}
}

func TestRemovedCleanupPercentIsUnknown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[cleanup]\nlow_disk_percent = 5\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "unknown key or section") || !strings.Contains(err.Error(), "cleanup.low_disk_percent") {
		t.Fatalf("expected unknown-key error: %v", err)
	}
}
