package hooks_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/hooks"
)

func TestWindowsAllHookTargetsRoundTrip(t *testing.T) {
	root := t.TempDir()
	for key, value := range map[string]string{"HOME": root, "USERPROFILE": root, "APPDATA": root, "LOCALAPPDATA": root, "DRIGA_DATA_DIR": filepath.Join(root, "data"), "DRIGA_SPOOL_DIR": filepath.Join(root, "events"), "DRIGA_CONFIG": filepath.Join(root, "config.toml")} {
		t.Setenv(key, value)
	}
	dir := filepath.Join(root, "tools with spaces 界's")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "driga.exe")
	if err := os.WriteFile(bin, []byte("inert executable fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, agent := range []string{"claude", "codex", "copilot"} {
		t.Run(agent, func(t *testing.T) {
			path := filepath.Join(root, agent+".config")
			before := ""
			if agent == "claude" {
				before = `{"other": 42}`
			}
			if agent == "codex" {
				before = "model = 'fixture'\n"
			}
			if before != "" {
				writeFixture(t, path, before)
			}
			var d hooks.Diff
			var err error
			switch agent {
			case "claude":
				d, err = hooks.ClaudeInstall(path, bin, true, testNow)
			case "codex":
				d, err = hooks.CodexInstall(path, bin, true, testNow)
			case "copilot":
				d, err = hooks.CopilotInstall(path, bin, true, testNow)
			}
			if err != nil || !d.Changed {
				t.Fatal(d, err)
			}
			if err := d.Apply(testNow); err != nil {
				t.Fatal(err)
			}
			switch agent {
			case "claude":
				events, err := hooks.ClaudeStatus(path)
				if err != nil || len(events) != 5 {
					t.Fatal(events, err)
				}
				_, err = hooks.ClaudeUninstall(path, false)
				if err != nil {
					t.Fatal(err)
				}
				if readFixture(t, path) != before {
					t.Fatal("Claude bytes changed")
				}
			case "codex":
				_, err = hooks.CodexUninstall(path, false)
				if err != nil || readFixture(t, path) != before {
					t.Fatal("Codex round-trip", err)
				}
			case "copilot":
				status, err := hooks.CopilotStatus(path, bin)
				if err != nil || status != "installed" {
					t.Fatal(status, err)
				}
				_, err = hooks.CopilotUninstall(path, false)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal(err)
				}
			}
			if agent != "copilot" && strings.Contains(readFixture(t, path), "hook ") {
				t.Fatal("owned hook left behind")
			}
		})
	}
}
