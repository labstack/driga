package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func doctorFixture(t *testing.T) (DoctorDeps, *store.Store) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	dir := filepath.Join(home, "data")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if err := st.SetMeta("last_pass_at", now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	return DoctorDeps{DataDir: dir, Store: st, Config: config.Default(), Home: home, Now: now, LockProbe: func(string) (bool, error) { return true, nil }}, st
}

func requireCheck(t *testing.T, checks []Check, name, status, fix string) Check {
	t.Helper()
	for _, c := range checks {
		if c.Name == name {
			if c.Status != status || (fix != "" && c.Fix != fix) {
				t.Fatalf("check %+v; want %s, %q", c, status, fix)
			}
			return c
		}
	}
	t.Fatalf("missing %s in %+v", name, checks)
	return Check{}
}

func TestDoctorNoDaemon(t *testing.T) {
	d, _ := doctorFixture(t)
	d.Store = nil
	d.LockProbe = func(string) (bool, error) { return false, nil }
	requireCheck(t, Doctor(context.Background(), d), "collector", "warn", "driga service install")
}

func TestDoctorIntake(t *testing.T) {
	d, st := doctorFixture(t)
	c := requireCheck(t, Doctor(context.Background(), d), "otlp intake", "info", "")
	if !strings.Contains(c.Detail, "off (optional") {
		t.Fatal(c)
	}
	d.Config.Intake.OTLP = true
	c = requireCheck(t, Doctor(context.Background(), d), "otlp intake", "info", "")
	if !strings.Contains(c.Detail, "no OTLP events since start") {
		t.Fatal(c)
	}
	for k, v := range map[string]string{"listen": "127.0.0.1:12345", "received": "3", "dropped": "5", "unmatched": "2"} {
		if err := st.SetMeta("otlp:"+k, v); err != nil {
			t.Fatal(err)
		}
	}
	c = requireCheck(t, Doctor(context.Background(), d), "otlp intake", "ok", "")
	for _, s := range []string{"127.0.0.1:12345", "events received: 3", "content attributes dropped: 5", "unmatched OTLP sessions: 2"} {
		if !strings.Contains(c.Detail, s) {
			t.Fatal(c)
		}
	}
	st.Tx(context.Background(), func(tx *store.Tx) error { return tx.SetSourceError("otlp", "address already in use", d.Now) })
	c = requireCheck(t, Doctor(context.Background(), d), "otlp intake", "warn", "")
	if !strings.Contains(c.Detail, "address already in use") {
		t.Fatal(c)
	}
}

func TestDoctorStaleSource(t *testing.T) {
	d, st := doctorFixture(t)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error { return tx.SetSourceOK("git", d.Now.Add(-16*time.Minute)) }); err != nil {
		t.Fatal(err)
	}
	requireCheck(t, Doctor(context.Background(), d), "source:git", "warn", "driga service restart")
}

func TestDoctorUntestedVersion(t *testing.T) {
	d, st := doctorFixture(t)
	if err := st.SetMeta("warn:codex:untested-version", "unknown release"); err != nil {
		t.Fatal(err)
	}
	c := requireCheck(t, Doctor(context.Background(), d), "version:codex", "warn", "")
	if !strings.Contains(c.Detail, "unknown release") {
		t.Fatal(c)
	}
}

func TestDoctorCadenceAndRSS(t *testing.T) {
	d, st := doctorFixture(t)
	heartbeat := filepath.Join(d.DataDir, "viewer.heartbeat")
	if err := os.WriteFile(heartbeat, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(heartbeat, d.Now, d.Now); err != nil {
		t.Fatal(err)
	}
	if err := st.SetMeta("last_pass_at", d.Now.Add(-16*time.Second).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		if err := tx.SetSourceOK("machine", d.Now.Add(-31*time.Second)); err != nil {
			return err
		}
		return tx.ReplaceProcs([]model.Proc{{PID: 1, Cmdline: "/opt/driga collect", RSS: 51_000_000}, {PID: 2, Cmdline: "editor driga collect", RSS: 1_000_000_000}})
	}); err != nil {
		t.Fatal(err)
	}
	checks := Doctor(context.Background(), d)
	requireCheck(t, checks, "collector", "warn", "driga service restart")
	requireCheck(t, checks, "source:machine", "warn", "driga service restart")
	c := requireCheck(t, checks, "collector-rss", "warn", "")
	if !strings.Contains(c.Detail, "51.0 MB") {
		t.Fatal(c)
	}
}

func TestDoctorPermissionsAndLockProbe(t *testing.T) {
	d, _ := doctorFixture(t)
	if err := os.Chmod(d.DataDir, 0755); err != nil {
		t.Fatal(err)
	}
	requireCheck(t, Doctor(context.Background(), d), "data-permissions", "fail", "chmod 700 "+d.DataDir)
	held, err := probeCollectorLock(d.DataDir)
	if err != nil || held {
		t.Fatalf("missing lock: %v %v", held, err)
	}
	if _, err := os.Stat(filepath.Join(d.DataDir, "driga.lock")); !os.IsNotExist(err) {
		t.Fatalf("probe created lock: %v", err)
	}
	lock, err := collector.Lock(d.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	held, err = probeCollectorLock(d.DataDir)
	if err != nil || !held {
		t.Fatalf("held lock: %v %v", held, err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	held, err = probeCollectorLock(d.DataDir)
	if err != nil || held {
		t.Fatalf("released lock: %v %v", held, err)
	}
}

func TestDoctorCollectorPaths(t *testing.T) {
	d, st := doctorFixture(t)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		return tx.ReplaceProcs([]model.Proc{
			{PID: 1, Exe: "/opt/path with spaces/driga", Cmdline: "/opt/path with spaces/driga collect", RSS: 10_000_000},
			{PID: 2, Cmdline: `"/opt/another path/driga" collect`, RSS: 10_000_000},
			{PID: 3, Exe: "/opt/editor", Cmdline: "driga collect", RSS: 1_000_000_000},
		})
	}); err != nil {
		t.Fatal(err)
	}
	c := requireCheck(t, Doctor(context.Background(), d), "collector-rss", "ok", "")
	if c.Detail != "20.0 MB" {
		t.Fatal(c)
	}
}

func TestDoctorCommandWarningsAndFailures(t *testing.T) {
	g, out := initFixture(t, "")
	g.JSON = true
	if err := (DoctorCmd{}).Run(g); err != nil {
		t.Fatal(err)
	}
	var checks []Check
	if err := json.Unmarshal(out.Bytes(), &checks); err != nil {
		t.Fatal(err)
	}
	requireCheck(t, checks, "collector", "warn", "driga service install")
	if _, err := os.Stat(config.DataDir()); !os.IsNotExist(err) {
		t.Fatalf("doctor wrote data directory: %v", err)
	}
	if err := os.Mkdir(config.DataDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(config.DataDir(), 0755); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := (DoctorCmd{}).Run(g); err == nil {
		t.Fatal("failed check must cause nonzero exit")
	}
	if err := json.Unmarshal(out.Bytes(), &checks); err != nil {
		t.Fatal(err)
	}
	requireCheck(t, checks, "data-permissions", "fail", "chmod 700 "+config.DataDir())
}

func TestDoctorLinuxChecks(t *testing.T) {
	d, _ := doctorFixture(t)
	d.OS, d.ProcRoot, d.LingerDir, d.Username, d.UID = "linux", t.TempDir(), t.TempDir(), "fixture-user", 1001
	checks := Doctor(context.Background(), d)
	c := requireCheck(t, checks, "psi", "info", "")
	if c.Detail != "psi n/a" {
		t.Fatal(c)
	}
	requireCheck(t, checks, "systemd-user", "warn", "")
	requireCheck(t, checks, "linger", "warn", "loginctl enable-linger $USER")
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(d.ProcRoot, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("pressure/memory", "some avg10=1.23 avg60=0.00 total=123\n")
	write("500/comm", "systemd\n")
	write("500/status", "Uid:\t1001\t1001\t1001\t1001\n")
	write("500/cmdline", "systemd\x00--user\x00")
	if err := os.WriteFile(filepath.Join(d.LingerDir, d.Username), nil, 0600); err != nil {
		t.Fatal(err)
	}
	checks = Doctor(context.Background(), d)
	requireCheck(t, checks, "psi", "ok", "")
	requireCheck(t, checks, "systemd-user", "ok", "")
	requireCheck(t, checks, "linger", "ok", "")
	d.OS = "darwin"
	for _, c := range Doctor(context.Background(), d) {
		if c.Name == "psi" || c.Name == "systemd-user" || c.Name == "linger" {
			t.Fatal(c)
		}
	}
}

func TestServiceLogsLinux(t *testing.T) {
	name, args := serviceLogCommand("linux", true)
	if name != "journalctl" || strings.Join(args, " ") != "--user -u run.driga.collector -n 50 -f" {
		t.Fatal(name, args)
	}
	name, args = serviceLogCommand("linux", false)
	if name != "journalctl" || strings.Contains(strings.Join(args, " "), "-f") {
		t.Fatal(name, args)
	}
}
