package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/inbox"
)

func TestWindowsActionCapabilities(t *testing.T) {
	root := t.TempDir()
	for key, value := range map[string]string{"HOME": root, "USERPROFILE": root, "APPDATA": root, "LOCALAPPDATA": root, "DRIGA_CONFIG": filepath.Join(root, "config.toml"), "DRIGA_DATA_DIR": filepath.Join(root, "data"), "DRIGA_SPOOL_DIR": filepath.Join(root, "events")} {
		t.Setenv(key, value)
	}
	checks := Doctor(context.Background(), DoctorDeps{OS: "windows", Home: root, DataDir: filepath.Join(root, "data"), Now: time.Now(), LockProbe: func(string) (bool, error) { return false, nil }})
	for _, name := range []string{"file probers", "Claude scratch"} {
		found := false
		for _, c := range checks {
			if c.Name == name && (strings.Contains(c.Detail, "unknown") || strings.Contains(c.Detail, "refused")) {
				found = true
			}
		}
		if !found {
			t.Fatal("missing doctor check", name)
		}
	}
	for _, name := range []string{"service", "hooks install", "notifications", "terminal jump", "kill", "cleanup apply", "socket probers"} {
		found := false
		for _, c := range checks {
			if c.Name == name && !strings.Contains(c.Detail, "not yet supported") {
				found = true
			}
		}
		if !found {
			t.Fatal("missing Windows capability check", name)
		}
	}
}

type windowsNoCLI struct{}

func (windowsNoCLI) Run(context.Context, string, ...string) ([]byte, error) {
	return nil, fmt.Errorf("test never launches agent CLIs")
}

func TestWindowsWatchCommands(t *testing.T) {
	root := t.TempDir()
	for key, value := range map[string]string{"HOME": root, "USERPROFILE": root, "APPDATA": root, "LOCALAPPDATA": root, "DRIGA_CONFIG": filepath.Join(root, "config.toml"), "DRIGA_DATA_DIR": filepath.Join(root, "data"), "DRIGA_SPOOL_DIR": filepath.Join(root, "events"), "CLAUDE_CONFIG_DIR": filepath.Join(root, ".claude"), "CODEX_HOME": filepath.Join(root, ".codex"), "COPILOT_HOME": filepath.Join(root, ".copilot"), "GEMINI_CLI_HOME": root} {
		t.Setenv(key, value)
	}
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte("no_service_hint = true\n[inbox]\nalerts = 'off'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	previous := newInboxRunner
	newInboxRunner = func() inbox.Runner { return windowsNoCLI{} }
	t.Cleanup(func() { newInboxRunner = previous })
	for _, args := range [][]string{{"collect", "--once"}, {"agents", "--snapshot", "--json"}, {"projects", "--snapshot", "--json"}, {"machine", "--snapshot", "--json"}, {"inbox", "--snapshot", "--json"}, {"doctor", "--json"}} {
		var out, errout bytes.Buffer
		if code := run(args, "test", strings.NewReader(""), &out, &errout); code != 0 {
			t.Fatalf("%v: %d %s %s", args, code, &out, &errout)
		}
	}
}
