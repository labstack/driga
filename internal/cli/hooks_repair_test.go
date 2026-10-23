package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/hooks"
)

func TestHooksRepairPreviewAndApply(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell fixture")
	}
	readEnv(t)
	t.Setenv("PATH", t.TempDir())
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	claude, codex, err := hookPaths()
	if err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(os.Getenv("HOME"), "old", "driga")
	for _, tc := range []struct {
		agent, path string
		install     func(string, string, bool, time.Time) (hooks.Diff, error)
	}{
		{"claude", claude, hooks.ClaudeInstall}, {"codex", codex, hooks.CodexInstall}, {"copilot", copilotHookPath(), hooks.CopilotInstall},
	} {
		if _, err := tc.install(tc.path, old, false, time.Now()); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(tc.path)
		if err != nil {
			t.Fatal(err)
		}
		var out, diagnostic bytes.Buffer
		args := []string{"hooks", "install", tc.agent, "--yes"}
		if code := run(append(args, "--dry-run"), "test", nil, &out, &diagnostic); code != 0 || !strings.Contains(out.String(), old) || !strings.Contains(out.String(), exe) {
			t.Fatalf("%s preview: %d %s %s", tc.agent, code, &out, &diagnostic)
		}
		after, err := os.ReadFile(tc.path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("preview wrote", err)
		}
		out.Reset()
		if code := run(args, "test", strings.NewReader("y\n"), &out, &diagnostic); code != 0 {
			t.Fatalf("apply: %d %s", code, &diagnostic)
		}
		status, detail, err := hookStatus(tc.agent, tc.path, exe)
		if err != nil || status != "ok" {
			t.Fatal(status, detail, err)
		}
	}
}
