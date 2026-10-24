package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/labstack/driga/internal/attention"
	"os"
	"path/filepath"
	"runtime"
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
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("COPILOT_HOME", filepath.Join(home, ".copilot"))
	t.Setenv("DRIGA_SPOOL_DIR", filepath.Join(home, "spool"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
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
	if err := os.WriteFile(filepath.Join(dir, "driga.lock"), []byte("1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return DoctorDeps{ProcessAlive: func(pid int32, _ time.Time) bool { return pid == 1 }, DataDir: dir, Store: st, Config: config.Default(), Home: home, Now: now, LockProbe: func(string) (bool, error) { return true, nil }}, st
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
	if runtime.GOOS == "windows" {
		t.Skip("Unix path or action contract; Windows phase 1 has dedicated watch/refusal tests")
	}

	d, _ := doctorFixture(t)
	d.Store = nil
	d.LockProbe = func(string) (bool, error) { return false, nil }
	requireCheck(t, Doctor(context.Background(), d), "collector", "warn", "driga service install")
}

func TestDoctorMacNotifier(t *testing.T) {
	for _, available := range []bool{false, true} {
		d, _ := doctorFixture(t)
		d.OS = "darwin"
		d.NotificationStatus = func() (string, error) {
			if !available {
				return "", fmt.Errorf("install Driga.app for native macOS notifications (unbundled builds cannot send them)")
			}
			return "native UserNotifications (Driga)", nil
		}
		status, detail, fix := "ok", "native UserNotifications (Driga)", ""
		if !available {
			status, detail, fix = "info", "install Driga.app for native macOS notifications (unbundled builds cannot send them)", "brew install --cask labstack/tap/driga"
		}
		c := requireCheck(t, Doctor(context.Background(), d), "notifications", status, fix)
		if c.Detail != detail || c.Fix != fix {
			t.Fatal(c)
		}
		d.OS = "linux"
		d.NotificationStatus = func() (string, error) { t.Fatal("macOS status on Linux"); return "", nil }
		for _, c := range Doctor(context.Background(), d) {
			if c.Name == "notifications" {
				t.Fatal(c)
			}
		}
	}
}

func TestDoctorMacNotificationsDenied(t *testing.T) {
	d, _ := doctorFixture(t)
	d.OS = "darwin"
	d.NotificationStatus = func() (string, error) { return "", attention.ErrNotificationsDenied }
	c := requireCheck(t, Doctor(context.Background(), d), "notifications", "warn", "System Settings > Notifications > Driga > Allow notifications")
	if c.Detail != attention.ErrNotificationsDenied.Error() {
		t.Fatal(c)
	}
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
	for k, v := range map[string]string{"listen": "127.0.0.1:12345", "received": "3", "accepted": "1", "records_rejected": "4", "metrics_rejected": "2", "dropped": "5", "unmatched": "2"} {
		if err := st.SetMeta("otlp:"+k, v); err != nil {
			t.Fatal(err)
		}
	}
	c = requireCheck(t, Doctor(context.Background(), d), "otlp intake", "ok", "")
	for _, s := range []string{"127.0.0.1:12345", "events received: 3", "content attributes dropped: 5", "unmatched OTLP sessions: 2", "accepted/matched records: 1", "rejected records: 4", "unused metric data points: 2"} {
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
	if runtime.GOOS == "windows" {
		t.Skip("Unix path or action contract; Windows phase 1 has dedicated watch/refusal tests")
	}

	d, st := doctorFixture(t)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error { return tx.SetSourceOK("git", d.Now.Add(-16*time.Minute)) }); err != nil {
		t.Fatal(err)
	}
	requireCheck(t, Doctor(context.Background(), d), "source:git", "warn", "driga service restart")
}

func TestDoctorIntakeRejections(t *testing.T) {
	d, st := doctorFixture(t)
	d.Config.Intake.OTLP = true
	for k, v := range map[string]string{"rejected": "6", "parse_failures": "4", "reject:invalid json": "4", "reject:invalid gzip": "2", "ignored": "8"} {
		if err := st.SetMeta("otlp:"+k, v); err != nil {
			t.Fatal(err)
		}
	}
	c := requireCheck(t, Doctor(context.Background(), d), "otlp intake", "warn", "")
	for _, want := range []string{"rejected: 6", "4 invalid json", "2 invalid gzip", "parse failures: 4", "ignored: 8"} {
		if !strings.Contains(c.Detail, want) {
			t.Fatalf("missing %q: %+v", want, c)
		}
	}
	if strings.Contains(c.Detail, "no OTLP events since start") {
		t.Fatal(c)
	}
}

func TestDoctorUntestedVersion(t *testing.T) {
	d, st := doctorFixture(t)
	if err := st.SetMeta("warn:codex:untested_version", "unknown release"); err != nil {
		t.Fatal(err)
	}
	c := requireCheck(t, Doctor(context.Background(), d), "version:codex", "warn", "")
	if !strings.Contains(c.Detail, "unknown release") {
		t.Fatal(c)
	}
}

func TestDoctorCadenceAndRSS(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix path or action contract; Windows phase 1 has dedicated watch/refusal tests")
	}

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
		return tx.ReplaceProcs([]model.Proc{{PID: 1, Cmdline: "/opt/driga collect", Argv: []string{"/opt/driga", "collect"}, RSS: 51_000_000}, {PID: 2, Cmdline: "editor driga collect", Argv: []string{"editor", "driga", "collect"}, RSS: 1_000_000_000}})
	}); err != nil {
		t.Fatal(err)
	}
	checks := Doctor(context.Background(), d)
	requireCheck(t, checks, "collector", "warn", "driga service restart")
	requireCheck(t, checks, "source:machine", "warn", "driga service restart")
	c := requireCheck(t, checks, "collector_rss", "warn", "")
	if !strings.Contains(c.Detail, "51.0 MB") {
		t.Fatal(c)
	}
}

func TestDoctorPermissionsAndLockProbe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix path or action contract; Windows phase 1 has dedicated watch/refusal tests")
	}

	d, _ := doctorFixture(t)
	if err := os.Remove(filepath.Join(d.DataDir, "driga.lock")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(d.DataDir, 0755); err != nil {
		t.Fatal(err)
	}
	requireCheck(t, Doctor(context.Background(), d), "data_permissions", "fail", "chmod 700 "+d.DataDir)
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
			{PID: 1, Exe: "/opt/path with spaces/driga", Cmdline: "/opt/path with spaces/driga collect", Argv: []string{"/opt/path with spaces/driga", "collect"}, RSS: 10_000_000},
			{PID: 2, Cmdline: `"/opt/another path/driga" collect`, Argv: []string{"/opt/another path/driga", "collect"}, RSS: 10_000_000},
			{PID: 3, Exe: "/opt/editor", Cmdline: "driga collect", Argv: []string{"driga", "collect"}, RSS: 1_000_000_000},
		})
	}); err != nil {
		t.Fatal(err)
	}
	c := requireCheck(t, Doctor(context.Background(), d), "collector_rss", "ok", "")
	if c.Detail != "10.0 MB" {
		t.Fatal(c)
	}
}

// The budget uses the platform's own-memory measure when one is recorded: RSS
// also counts clean, shared code pages and swings with each GC cycle.
func TestDoctorCollectorFootprint(t *testing.T) {
	for _, tc := range []struct {
		footprint, rss uint64
		status, detail string
	}{
		{40_000_000, 58_000_000, "ok", "40.0 MB footprint (RSS 58.0 MB)"},
		{51_000_000, 58_000_000, "warn", "51.0 MB footprint (RSS 58.0 MB)"},
	} {
		d, st := doctorFixture(t)
		if err := st.Tx(context.Background(), func(tx *store.Tx) error {
			return tx.ReplaceProcs([]model.Proc{{PID: 1, Cmdline: "/opt/driga collect", Argv: []string{"/opt/driga", "collect"}, Footprint: tc.footprint, MemoryMeasure: "physical_footprint", RSS: tc.rss}})
		}); err != nil {
			t.Fatal(err)
		}
		c := requireCheck(t, Doctor(context.Background(), d), "collector_rss", tc.status, "")
		if c.Detail != tc.detail {
			t.Fatalf("detail %q, want %q", c.Detail, tc.detail)
		}
	}
}

func TestDoctorCommandWarningsAndFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix path or action contract; Windows phase 1 has dedicated watch/refusal tests")
	}

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
	if err := (DoctorCmd{}).Run(g); err == nil || !strings.Contains(err.Error(), "data_permissions") || !strings.Contains(err.Error(), "chmod 700 "+config.DataDir()) {
		t.Fatalf("failed checks must reach the CLI error handler with guidance: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("command must leave failure framing to Main: %s", out)
	}
}

func TestDoctorLinuxChecks(t *testing.T) {
	d, _ := doctorFixture(t)
	d.OS, d.ProcRoot, d.LingerDir, d.Username, d.UID = "linux", t.TempDir(), t.TempDir(), "fixture-user", 1001
	checks := Doctor(context.Background(), d)
	c := requireCheck(t, checks, "psi", "info", "")
	if c.Detail != "psi n/a" {
		t.Fatal(c)
	}
	requireCheck(t, checks, "systemd_user", "warn", "")
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
	requireCheck(t, checks, "systemd_user", "ok", "")
	requireCheck(t, checks, "linger", "ok", "")
	d.OS = "darwin"
	for _, c := range Doctor(context.Background(), d) {
		if c.Name == "psi" || c.Name == "systemd_user" || c.Name == "linger" {
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

func TestDoctorDatabaseSizeBudget(t *testing.T) {
	d, _ := doctorFixture(t)
	path := filepath.Join(d.DataDir, "driga.db")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(store.StorageBudgetBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	check := requireCheck(t, Doctor(context.Background(), d), "storage:db", "warn", "")
	for _, want := range []string{"500000000", "largest table", "including indexes"} {
		if !strings.Contains(check.Detail, want) {
			t.Fatalf("missing %q: %+v", want, check)
		}
	}
	// The WAL also consumes the budget, even when the database alone is small.
	if err := os.Truncate(path, 1); err != nil {
		t.Fatal(err)
	}
	wal, err := os.Create(path + "-wal")
	if err != nil {
		t.Fatal(err)
	}
	if err := wal.Truncate(store.StorageBudgetBytes); err != nil {
		t.Fatal(err)
	}
	wal.Close()
	requireCheck(t, Doctor(context.Background(), d), "storage:db", "warn", "")
}

func TestDoctorMalformedExcludeWarning(t *testing.T) {
	d, _ := doctorFixture(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("exclude = ['foo[', '**/node_modules']\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	d.Config = cfg
	c := requireCheck(t, Doctor(context.Background(), d), "config", "warn", "")
	if !strings.Contains(c.Detail, "exclude") || !strings.Contains(c.Detail, "foo[") || !strings.Contains(c.Detail, path) {
		t.Fatal(c)
	}
	if len(cfg.Exclude) != 1 || cfg.Exclude[0] != "**/node_modules" {
		t.Fatal(cfg.Exclude)
	}
}
