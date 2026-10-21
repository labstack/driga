package hooks_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/labstack/driga/internal/hooks"
)

func TestCopilotOwnedFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("COPILOT_HOME", filepath.Join(home, "custom"))
	t.Setenv("DRIGA_DATA_DIR", filepath.Join(home, "data"))
	t.Setenv("DRIGA_SPOOL_DIR", filepath.Join(home, "spool"))
	t.Setenv("DRIGA_CONFIG", filepath.Join(home, "config.toml"))
	path := hooks.CopilotPath(home, os.Getenv("COPILOT_HOME"))
	if path != filepath.Join(home, "custom", "hooks", "driga.json") {
		t.Fatal(path)
	}
	if hooks.CopilotPath(home, "") != filepath.Join(home, ".copilot", "hooks", "driga.json") {
		t.Fatal("fallback")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(filepath.Dir(path), "user.json")
	if err := os.WriteFile(other, []byte("user-owned"), 0600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "tools with spaces", "driga")
	if err := os.MkdirAll(filepath.Dir(binary), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, nil, 0700); err != nil {
		t.Fatal(err)
	}
	d, err := hooks.CopilotInstall(path, binary, true, time.Now())
	if err != nil || !d.Changed {
		t.Fatal(d, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("dry run wrote", err)
	}
	if err := d.Apply(time.Now()); err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Version int
		Hooks   map[string][]struct {
			Type, Exec string
			Args       []string
			TimeoutSec int
		}
	}
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	events := []string{"sessionStart", "userPromptSubmitted", "postToolUse", "postToolUseFailure", "notification", "agentStop", "errorOccurred", "subagentStart", "subagentStop", "sessionEnd"}
	if cfg.Version != 1 || len(cfg.Hooks) != len(events) {
		t.Fatal(string(b))
	}
	for _, ev := range events {
		es := cfg.Hooks[ev]
		if len(es) != 1 {
			t.Fatal(ev, es)
		}
		e := es[0]
		if e.Type != "command" || e.Exec != binary || e.TimeoutSec != 3 || len(e.Args) != 3 || e.Args[0] != "hook" || e.Args[1] != "copilot" || e.Args[2] != ev {
			t.Fatal(e)
		}
	}
	d, err = hooks.CopilotInstall(path, binary, false, time.Now())
	if err != nil || d.Changed {
		t.Fatal(d, err)
	}
	status, err := hooks.CopilotStatus(path, binary)
	if err != nil || status != "installed" {
		t.Fatal(status, err)
	}
	status, err = hooks.CopilotStatus(path, "/new/driga")
	if err != nil || status != "installed" {
		t.Fatal(status, err)
	}
	d, err = hooks.CopilotUninstall(path, true)
	if err != nil || !d.Changed || d.After != "" {
		t.Fatal(d, err)
	}
	if err := d.Apply(time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	files, _ := os.ReadDir(filepath.Dir(path))
	if len(files) != 1 || files[0].Name() != "user.json" {
		t.Fatal(files)
	}
	b, _ = os.ReadFile(other)
	if string(b) != "user-owned" {
		t.Fatal(string(b))
	}
	status, err = hooks.CopilotStatus(path, "/new/driga")
	if status != "missing" || err != nil {
		t.Fatal(status, err)
	}
}

func TestCopilotPreviewGuardsAndRepair(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix symlink fixture; Windows watch tests require no Developer Mode or admin rights")
	}

	path := filepath.Join(t.TempDir(), "hooks", "driga.json")
	if _, err := hooks.CopilotInstall(path, "relative/driga", false, time.Now()); err == nil {
		t.Fatal("accepted relative executable")
	}
	if _, err := hooks.CopilotInstall(path, "/old/driga", false, time.Now()); err != nil {
		t.Fatal(err)
	}
	d, err := hooks.CopilotInstall(path, "/new/driga", true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"edited":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := d.Apply(time.Now()); err == nil {
		t.Fatal("overwrote concurrent edit")
	}
	if _, err := hooks.CopilotInstall(path, "/new/driga", false, time.Now()); err != nil {
		t.Fatal(err)
	}
	status, err := hooks.CopilotStatus(path, "/new/driga")
	if status != "stale" || err != nil {
		t.Fatal(status, err)
	}
	other := filepath.Join(t.TempDir(), "user.json")
	if err := os.WriteFile(other, []byte("user"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, path); err != nil {
		t.Fatal(err)
	}
	if _, err := hooks.CopilotInstall(path, "/new/driga", false, time.Now()); err == nil {
		t.Fatal("followed owned symlink")
	}
	if _, err := hooks.CopilotUninstall(path, false); err == nil {
		t.Fatal("followed owned symlink")
	}
	b, _ := os.ReadFile(other)
	if string(b) != "user" {
		t.Fatal(string(b))
	}
}

func TestCopilotStatusOwnership(t *testing.T) {
	path := filepath.Join(t.TempDir(), "driga.json")
	for _, tc := range []struct {
		name   string
		exists bool
		want   string
	}{
		{"driga", true, "installed"}, {"other", true, "stale"}, {"driga", false, "stale"},
	} {
		binary := filepath.Join(t.TempDir(), tc.name)
		if tc.exists {
			if err := os.WriteFile(binary, nil, 0700); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := hooks.CopilotInstall(path, binary, false, time.Now()); err != nil {
			t.Fatal(err)
		}
		got, err := hooks.CopilotStatus(path, "/different/driga")
		if err != nil || got != tc.want {
			t.Fatal(tc, got, err)
		}
	}
}

func TestCopilotStatusDoesNotCallConfigurationDriftStale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "driga.json")
	binary := filepath.Join(t.TempDir(), "driga")
	if err := os.WriteFile(binary, nil, 0700); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"version": 1, "hooks": map[string]any{"sessionStart": []any{map[string]any{"exec": binary, "timeoutSec": 5, "args": []string{"hook", "copilot", "sessionStart"}}}}})
	for _, tc := range []struct {
		body []byte
		want string
	}{{b, "installed"}, {[]byte(`{}`), "missing"}, {[]byte(`{`), "missing"}} {
		if err := os.WriteFile(path, tc.body, 0600); err != nil {
			t.Fatal(err)
		}
		if got, err := hooks.CopilotStatus(path, "/another/driga"); err != nil || got != tc.want {
			t.Fatal(string(tc.body), got, err)
		}
	}
}
