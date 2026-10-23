package cli

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/fleet"
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
			{PID: 42, StartTime: start, Cmdline: "/opt/driga collect", Argv: []string{"/opt/driga", "collect"}, RSS: 20_000_000},
			{PID: 43, StartTime: start, Cmdline: "/debug/driga collect", Argv: []string{"/debug/driga", "collect"}, RSS: 90_000_000},
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
	// A renamed binary (a development build) is still the lock owner.
	d.ProcessAlive = func(pid int32, at time.Time) bool { return pid == 42 && at.Equal(start) }
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		return tx.ReplaceProcs([]model.Proc{{PID: 42, StartTime: start, Exe: "/opt/driga-dev", Cmdline: "/opt/driga-dev collect", Argv: []string{"/opt/driga-dev", "collect"}, RSS: 20_000_000}})
	}); err != nil {
		t.Fatal(err)
	}
	if c = requireCheck(t, Doctor(context.Background(), d), "collector_rss", "ok", ""); c.Detail != "20.0 MB" {
		t.Fatal(c)
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

func TestDoctorSocketPathsAndFleetErrors(t *testing.T) {
	d, st := doctorFixture(t)
	const detail = "fleet /v1/snapshot: 503 Service Unavailable: snapshot unavailable: invalid uri authority: a"
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		if err := tx.SetSourceError("chat", "chat socket path is not a socket", d.Now); err != nil {
			return err
		}
		return tx.ForMachine("remote").UpsertMachine(model.Machine{Name: "peer-a", LastError: detail})
	}); err != nil {
		t.Fatal(err)
	}
	checks := Doctor(context.Background(), d)
	c := requireCheck(t, checks, "chat socket", "warn", "")
	if !strings.Contains(c.Detail, chat.SocketPath(d.DataDir)) || !strings.Contains(c.Detail, "not a socket") {
		t.Fatal(c)
	}
	c = requireCheck(t, checks, "fleet socket", "info", "")
	if !strings.Contains(c.Detail, fleet.ControlSocketPath(d.DataDir)) {
		t.Fatal(c)
	}
	c = requireCheck(t, checks, "fleet:peer-a", "warn", "")
	if !strings.Contains(c.Detail, detail) {
		t.Fatal(c)
	}
}

func TestDoctorReportsSchemaReset(t *testing.T) {
	d, _ := doctorFixture(t)
	t.Setenv("DRIGA_DATA_DIR", d.DataDir)
	configPath := filepath.Join(d.Home, "config.toml")
	t.Setenv("DRIGA_CONFIG", configPath)
	if err := os.WriteFile(configPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(d.DataDir, "driga.db")
	st, err := store.Open(path, store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA user_version=0"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	_, schemaErr := store.Open(path, store.ReadOnly)
	if schemaErr == nil {
		t.Fatal("expected pre-release schema")
	}
	var out bytes.Buffer
	err = (DoctorCmd{}).Run(&Globals{Stdout: &out})
	if err == nil {
		t.Fatal("doctor accepted incompatible database")
	}
	t.Logf("doctor error: %v", err)
	if !strings.Contains(out.String(), schemaErr.Error()) {
		t.Fatalf("doctor omitted shared schema diagnosis: %s", out.String())
	}
}

func TestDoctorWarnsAboutStaleMacOSService(t *testing.T) {
	d, _ := doctorFixture(t)
	d.OS = "darwin"
	d.ServicePlistStale = func() (bool, error) { return true, nil }
	requireCheck(t, Doctor(context.Background(), d), "service", "warn", "driga service install")
}
