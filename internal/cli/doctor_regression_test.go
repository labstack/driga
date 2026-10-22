package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/hooks"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestDoctorQuietEventSources(t *testing.T) {
	d, st := doctorFixture(t)
	for _, name := range []string{"sockets", "otlp"} {
		if err := st.Tx(context.Background(), func(tx *store.Tx) error { return tx.SetSourceOK(name, d.Now.Add(-12*time.Minute)) }); err != nil {
			t.Fatal(err)
		}
	}
	c := requireCheck(t, Doctor(context.Background(), d), "source:sockets", "ok", "")
	if c.Fix != "" || !strings.Contains(c.Detail, "last event") {
		t.Fatal(c)
	}
	if err := st.Tx(context.Background(), func(tx *store.Tx) error { return tx.SetSourceError("sockets", "probe failed", d.Now) }); err != nil {
		t.Fatal(err)
	}
	requireCheck(t, Doctor(context.Background(), d), "source:sockets", "warn", "driga service restart")
}

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
		if err := (HooksStatusCmd{HooksOptions{Target: agent}}).Run(&Globals{Stdout: &out}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "stale") || !strings.Contains(out.String(), "driga hooks install "+agent) {
			t.Fatal(out.String())
		}
	}
}

func TestDoctorCollectorBudgetUsesLockOwner(t *testing.T) {
	d, st := doctorFixture(t)
	start := d.Now.Add(-time.Hour)
	if err := os.WriteFile(filepath.Join(d.DataDir, "driga.lock"), []byte("42\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d.ProcessAlive = func(pid int32, at time.Time) bool { return pid == 42 && at.Equal(start) }
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		return tx.ReplaceProcs([]model.Proc{
			{PID: 42, StartTime: start, Cmdline: "/opt/driga collect", RSS: 20_000_000},
			{PID: 43, StartTime: start, Cmdline: "/debug/driga collect", RSS: 90_000_000},
		})
	}); err != nil {
		t.Fatal(err)
	}
	checks := Doctor(context.Background(), d)
	c := requireCheck(t, checks, "collector_rss", "ok", "")
	if c.Detail != "20.0 MB" || c.Fix != "" {
		t.Fatal(c)
	}
	requireCheck(t, checks, "other_collectors", "info", "")
	d.ProcessAlive = func(int32, time.Time) bool { return false }
	c = requireCheck(t, Doctor(context.Background(), d), "collector_rss", "warn", "")
	if strings.Contains(c.Detail, "20.0 MB") {
		t.Fatal("reused/dead PID counted", c)
	}
}

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
		args := []string{"hooks", "install", tc.agent}
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
