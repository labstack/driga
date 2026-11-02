//go:build linux

package config_test

import (
	"path/filepath"
	"testing"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/variant"
)

func TestDataDirLinuxXDG(t *testing.T) {
	t.Cleanup(variant.SetForTest("")) // a release build
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DRIGA_DATA_DIR", "")
	t.Setenv("XDG_DATA_HOME", "")
	if got := config.DataDir(); got != filepath.Join(home, ".local/share/driga") {
		t.Fatal(got)
	}
	xdg := filepath.Join(home, "xdg data")
	t.Setenv("XDG_DATA_HOME", xdg)
	if got := config.DataDir(); got != filepath.Join(xdg, "driga") {
		t.Fatal(got)
	}
	t.Setenv("DRIGA_DATA_DIR", filepath.Join(home, "override"))
	if got := config.DataDir(); got != filepath.Join(home, "override") {
		t.Fatal(got)
	}
}
