package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/hooks"
)

func TestDoctorHookDetailsAndStalePaths(t *testing.T) {
	d, _ := doctorFixture(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	codex := filepath.Join(d.Home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(codex), 0700); err != nil {
		t.Fatal(err)
	}
	foreign := "notify = ['another-tool', 'notify']\n"
	if err := os.WriteFile(codex, []byte(foreign), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := hooks.CodexInstall(codex, exe, false, d.Now); err != nil {
		t.Fatal(err)
	}
	c := requireCheck(t, Doctor(context.Background(), d), "hooks:codex", "ok", "")
	if !strings.Contains(c.Detail, "PermissionRequest") || strings.Contains(c.Detail, "notify") {
		t.Fatal(c)
	}
	for _, agent := range []string{"claude", "codex", "copilot"} {
		path := filepath.Join(d.Home, ".claude", "settings.json")
		install := hooks.ClaudeInstall
		switch agent {
		case "codex":
			path = codex
			install = hooks.CodexInstall
		case "copilot":
			path = hooks.CopilotPath(d.Home, "")
			install = hooks.CopilotInstall
		}
		if _, err := install(path, filepath.Join(d.Home, "old", "driga"), false, d.Now); err != nil {
			t.Fatal(err)
		}
		requireCheck(t, Doctor(context.Background(), d), "hooks:"+agent, "warn", "driga hooks install "+agent)
		var out strings.Builder
		if err := (HooksStatusCmd{Target: agent}).Run(&Globals{Stdout: &out}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "stale") || !strings.Contains(out.String(), "driga hooks install "+agent) {
			t.Fatal(out.String())
		}
	}
}
