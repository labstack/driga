package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFlowMotionConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("motion = \"off\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil || c.Motion != "off" {
		t.Fatal(c.Motion, err)
	}
	t.Setenv("DRIGA_MOTION", "on")
	c, err = Load(path)
	if err != nil || c.Motion != "on" {
		t.Fatal(c.Motion, err)
	}
	t.Setenv("DRIGA_MOTION", "sometimes")
	if _, err = Load(path); err == nil {
		t.Fatal("invalid motion accepted")
	}
}
