package hooks_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/hooks"
)

func TestClaudePromptInstallRepairUninstallDiff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	bin := testBinary("/opt/driga")
	writeFixture(t, path, `{"keep":true}`)
	install, err := hooks.ClaudeInstall(path, bin, true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(install.After, "PermissionRequest") || !strings.Contains(install.After, `"timeout": 660`) {
		t.Fatal(install.After)
	}
	if readFixture(t, path) != `{"keep":true}` {
		t.Fatal("preview wrote settings")
	}
	if err = install.Apply(time.Now()); err != nil {
		t.Fatal(err)
	}
	// A stale timeout goes through the same preview and approval path.
	stale := strings.ReplaceAll(install.After, `"timeout": 660`, `"timeout": 12`)
	if err = os.WriteFile(path, []byte(stale), 0600); err != nil {
		t.Fatal(err)
	}
	repair, err := hooks.ClaudeInstall(path, bin, true, time.Now())
	if err != nil || !repair.Changed || !strings.Contains(repair.After, `"timeout": 660`) {
		t.Fatal(repair, err)
	}
	if readFixture(t, path) != stale {
		t.Fatal("repair preview wrote settings")
	}
	if err = repair.Apply(time.Now()); err != nil {
		t.Fatal(err)
	}
	uninstall, err := hooks.ClaudeUninstall(path, true)
	if err != nil || !uninstall.Changed || strings.Contains(uninstall.After, "PermissionRequest") {
		t.Fatal(uninstall, err)
	}
	if err = uninstall.Apply(time.Now()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readFixture(t, path), `"keep":true`) {
		t.Fatal("lost unrelated setting")
	}
}
