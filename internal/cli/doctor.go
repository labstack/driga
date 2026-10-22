package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	ks "github.com/kardianos/service"
	"github.com/labstack/driga/internal/filelock"
	"github.com/labstack/driga/internal/secfile"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/hooks"
	"github.com/labstack/driga/internal/inbox"
	"github.com/labstack/driga/internal/intake"
	"github.com/labstack/driga/internal/logfile"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/service"
	"github.com/labstack/driga/internal/signatures"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

// Check is an actionable setup or collection diagnostic.
type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	Fix    string `json:"fix"`
}

// DoctorReader is the small read-only portion of the store used by diagnostics.
type DoctorReader interface {
	Sources() ([]model.SourceStatus, error)
	Meta(string) (string, error)
	Procs() ([]model.Proc, error)
}

// DoctorDeps supplies all environment and collection state for diagnostics.
type DoctorDeps struct {
	OS                 string // Optional OS override for offline diagnostics.
	ProcRoot           string // Optional procfs root; defaults to /proc.
	LingerDir          string // Optional linger marker directory.
	UID                int
	Username           string
	DataDir            string
	Store              DoctorReader
	Config             config.Config
	Home               string
	CodexHome          string // Optional override; defaults to Home/.codex.
	CopilotHome        string // Optional override; defaults to Home/.copilot.
	Now                time.Time
	ProcessAlive       func(int32, time.Time) bool // Optional PID/start validation for fixtures.
	LockProbe          func(dataDir string) (held bool, err error)
	NotifierLookup     func() string          // Optional cached notifier lookup for fixtures.
	ServiceState       func() (string, error) // Windows Task Scheduler, read-only.
	NotificationStatus func() (string, error) // Windows Runtime, no registration/display.
}

// Doctor checks stored evidence without collecting, writing, or controlling launchd.
func Doctor(ctx context.Context, d DoctorDeps) []Check {
	goos := d.OS
	if goos == "" {
		goos = runtime.GOOS
	}
	checks := []Check{}
	copilotSeen := false
	add := func(name, status, detail, fix string) { checks = append(checks, Check{name, status, detail, fix}) }
	for _, entry := range []struct{ name, path string }{
		{"storage:db", filepath.Join(d.DataDir, "driga.db")},
		{"storage:wal", filepath.Join(d.DataDir, "driga.db-wal")},
		{"storage:spool", config.SpoolDir()},
		{"storage:log", service.LogPath()},
		{"storage:cleanup_log", filepath.Join(d.DataDir, "cleanup.log")},
	} {
		size, capped, err := cleanup.WalkSize(ctx, entry.path)
		status, detail := "info", fmt.Sprintf("%d bytes", size)
		if capped {
			detail = "≥" + detail
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			status = "warn"
			detail = err.Error()
		}
		if strings.Contains(entry.name, "log") {
			detail += fmt.Sprintf("; cap %d bytes (10 MiB), one rotated file", logfile.MaxSize)
		}
		add(entry.name, status, detail, "")
	}
	if err := ctx.Err(); err != nil {
		add("doctor", "fail", err.Error(), "rerun driga doctor")
		return checks
	}
	intervals := collector.Cadence(time.Hour)
	if info, err := os.Stat(filepath.Join(d.DataDir, "viewer.heartbeat")); err == nil {
		intervals = collector.Cadence(d.Now.Sub(info.ModTime()))
	}
	probe := d.LockProbe
	if probe == nil {
		probe = probeCollectorLock
	}
	held, err := probe(d.DataDir)
	switch {
	case err != nil:
		add("collector", "fail", err.Error(), "check access to "+d.DataDir+"/driga.lock")
	case !held:
		add("collector", "warn", "not running; snapshot mode has no history", "driga service install")
	case d.Store == nil:
		add("collector", "warn", "lock held but database unavailable", "driga service restart")
	default:
		raw, err := d.Store.Meta("last_pass_at")
		last, parseErr := time.Parse(time.RFC3339Nano, raw)
		if err != nil || parseErr != nil || last.After(d.Now) || d.Now.Sub(last) > 3*intervals.Procs {
			add("collector", "warn", "lock held but last pass is missing or stale", "driga service restart")
		} else {
			add("collector", "ok", "running; last pass "+d.Now.Sub(last).Round(time.Second).String()+" ago", "")
		}
	}
	if d.Store != nil {
		sources, err := d.Store.Sources()
		if err != nil {
			add("sources", "fail", err.Error(), "check database access; run driga service restart")
		}
		for _, source := range sources {
			if source.Name == "otlp" {
				continue
			} // Event-driven intake has no polling freshness deadline.
			fresh := view.SourceFreshness(source, "daemon", d.Now, intervals)
			age := time.Duration(fresh.AgeMS) * time.Millisecond
			status, fix := "ok", ""
			detail := "last success " + age.Round(time.Second).String() + " ago"
			if fresh.Status == "last_event" {
				detail = "last event " + age.Round(time.Second).String() + " ago"
			}
			if source.LastOKAt.IsZero() {
				detail = "no successful collection"
			}
			if fresh.Status == "not_yet_receiving" {
				detail = "not yet receiving events"
			}
			if fresh.Stale || fresh.Status == "" && (source.LastOKAt.After(d.Now) || source.LastErrorAt.After(source.LastOKAt)) {
				status, fix = "warn", "driga service restart"
			}
			if source.LastError != "" {
				detail += "; last error: " + source.LastError
			}

			add("source:"+source.Name, status, detail, fix)
		}
		procs, err := d.Store.Procs()
		if err != nil {
			add("collector_rss", "fail", err.Error(), "check database access; run driga service restart")
		} else {
			sigs, _ := signatures.Load(nil)
			for _, p := range procs {
				sig, ok := sigs.Match(p.Cmdline, p.Exe)
				if p.Agent == "copilot" || ok && sig.Agent == "copilot" {
					copilotSeen = true
				}
			}
			for _, p := range procs {
				sig, ok := sigs.Match(p.Cmdline, p.Exe)
				if p.Agent == "agy" || ok && sig.Agent == "agy" {
					add("agy", "info", "process-level (schema not yet supported)", "")
					break
				}
			}
			// The 50 MB budget is the platform's own-memory measure (physical
			// footprint on macOS, private bytes on Windows) when recorded. RSS
			// also counts clean, shared code pages and swings with each GC.
			var rss, footprint, budget uint64
			found, measured := false, false
			owner, _ := os.ReadFile(filepath.Join(d.DataDir, "driga.lock"))
			ownerPID, _ := strconv.ParseInt(strings.TrimSpace(string(owner)), 10, 32)
			alive := d.ProcessAlive
			if alive == nil {
				alive = platform.New().Alive
			}
			others := 0
			for _, p := range procs {
				if !isCollectorProc(p) {
					continue
				}
				if !held || p.PID <= 0 || int64(p.PID) != ownerPID || !alive(p.PID, p.StartTime) {
					others++
					continue
				}
				rss = p.RSS
				if p.Footprint > 0 {
					footprint, budget, measured = p.Footprint, p.Footprint, true
				} else {
					budget = p.RSS
				}
				found = true
			}
			if others > 0 {
				add("other_collectors", "info", fmt.Sprintf("%d other collector processes; excluded from this data directory's memory budget", others), "")
			}

			status, fix, detail := "ok", "", fmt.Sprintf("%.1f MB", float64(rss)/1e6)
			if measured {
				detail = fmt.Sprintf("%.1f MB footprint (RSS %.1f MB)", float64(footprint)/1e6, float64(rss)/1e6)
			}
			if !found {
				status, detail, fix = "warn", "collector absent from process snapshot", "driga service start"
			} else if budget > 50_000_000 {
				status, fix = "warn", "driga service restart; inspect driga service logs"
			}
			add("collector_rss", status, detail, fix)
		}
	}
	if info, err := os.Stat(filepath.Join(d.Home, ".gemini", "antigravity-cli")); err == nil && info.IsDir() {
		found := false
		for _, c := range checks {
			found = found || c.Name == "agy"
		}
		if !found {
			add("agy", "info", "process-level (schema not yet supported)", "")
		}
	}
	codexHome := d.CodexHome
	if codexHome == "" {
		codexHome = filepath.Join(d.Home, ".codex")
	}
	names := make([]string, 0, len(d.Config.Adapters))
	for name := range d.Config.Adapters {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !d.Config.Adapters[name].Enabled {
			add("adapter:"+name, "ok", "disabled in config", "")
			continue
		}
		var dirs []string
		if name == "copilot" {
			if warning := copilotVersionWarning(ctx); warning != "" {
				add("version:copilot", "warn", warning, "upgrade Copilot CLI to 1.0.98 or newer")
			}
			if copilotSandboxEnabled(d.Home, d.CopilotHome) {
				add("sandbox:copilot", "warn", "sandbox.enabled is true; sandboxed hooks may be unable to reach Driga's spool", "allow the Driga spool in the Copilot sandbox or disable the sandbox")
			}
			if copilotSeen {
				received := ""
				if d.Store != nil {
					received, _ = d.Store.Meta("hooks:copilot:received")
				}
				if received == "" || received == "0" {
					add("events:copilot", "warn", "Copilot processes seen but no Copilot hook events received; check sandbox, disableAllHooks, and sessions started before installation", "driga hooks install copilot; restart running Copilot sessions")
				}
			}
			exe, err := os.Executable()
			status, detail := "fail", ""
			if err == nil {
				status, detail, err = hookStatus("copilot", hooks.CopilotPath(d.Home, d.CopilotHome), stablePath(exe))
			}
			fix := "driga hooks install copilot"
			if err != nil {
				detail = err.Error()
			} else if status == "ok" {
				fix = ""
			}
			add("hooks:copilot", status, detail, fix)
			continue
		}
		switch name {
		case "claude":
			claudeHome := os.Getenv("CLAUDE_CONFIG_DIR")
			if claudeHome == "" {
				claudeHome = filepath.Join(d.Home, ".claude")
			}
			dirs = []string{filepath.Join(claudeHome, "sessions"), filepath.Join(claudeHome, "projects")}
		case "copilot":
			copilotHome := d.CopilotHome
			if copilotHome == "" {
				copilotHome = filepath.Join(d.Home, ".copilot")
			}
			dirs = []string{filepath.Join(copilotHome, "session-state")}
		case "codex":
			dirs = []string{filepath.Join(codexHome, "sessions")}
		case "gemini":
			home := os.Getenv("GEMINI_CLI_HOME")
			if home == "" {
				home = d.Home
			}
			dirs = []string{filepath.Join(home, ".gemini", "tmp")}
		}
		found := false
		for _, dir := range dirs {
			if info, err := os.Stat(dir); err == nil && info.IsDir() {
				found = true
			}
		}
		if found {
			add("adapter:"+name, "ok", "session directory found", "")
		} else {
			add("adapter:"+name, "warn", "session directory not found", "start a "+name+" session or disable [adapters."+name+"] in driga config edit")
		}
		if d.Store != nil {
			warning, err := d.Store.Meta("warn:" + name + ":untested_version")
			if err == nil && warning != "" {
				add("version:"+name, "warn", "untested version; tolerant parsing: "+warning, "update driga to a release tested with this agent version")
			} else if err != nil && !errors.Is(err, store.ErrNotFound) {
				add("version:"+name, "fail", err.Error(), "check database access")
			}
		}
		if name == "claude" || name == "codex" {
			path := filepath.Join(codexHome, "config.toml")
			if name == "claude" {
				claudeHome := filepath.Join(d.Home, ".claude")
				if goos == "windows" && os.Getenv("CLAUDE_CONFIG_DIR") != "" {
					claudeHome = os.Getenv("CLAUDE_CONFIG_DIR")
				}
				path = filepath.Join(claudeHome, "settings.json")
			}
			exe, err := os.Executable()
			status, detail := "fail", ""
			if err == nil {
				status, detail, err = hookStatus(name, path, stablePath(exe))
			}
			fix := "driga hooks install " + name
			if err != nil {
				detail = err.Error()
			} else if status == "ok" {
				fix = ""
			}
			add("hooks:"+name, status, detail, fix)
		}
	}
	info, err := os.Stat(d.DataDir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		add("data_permissions", "warn", "data directory does not exist", "driga service install")
	case err != nil:
		add("data_permissions", "fail", err.Error(), "check access to "+d.DataDir)
	case !info.IsDir():
		add("data_permissions", "fail", "data path is not a directory", "set DRIGA_DATA_DIR to a private directory")
	case goos == "windows":
		if err := secfile.CheckPrivate(d.DataDir); err != nil {
			add("data_permissions", "fail", err.Error(), "set DRIGA_DATA_DIR to a private directory")
		} else {
			add("data_permissions", "ok", "private Windows DACL", "")
		}
	case info.Mode().Perm() != 0700:
		add("data_permissions", "fail", fmt.Sprintf("directory permissions are %04o; expected 0700", info.Mode().Perm()), "chmod 700 "+d.DataDir)
	default:
		add("data_permissions", "ok", "0700", "")
	}
	if goos == "windows" {
		add("kill", "ok", "proven orphan trees; PID-specific WM_CLOSE, grace period, optional TerminateProcess", "")
		add("cleanup apply", "ok", "verified worktrees to Recycle Bin (IFileOperation); confirmation and audit log required", "")
		add("socket probers", "ok", "IP Helper TCP/UDP owner tables (IPv4 and IPv6); probe errors leave evidence unknown", "")
		add("file probers", "ok", "Restart Manager; bounded file batches, probe errors leave evidence unknown", "")
		add("Claude scratch", "info", "refused: Windows scratch layout unverified in repository fixtures", "")
		state := "per-user Task Scheduler (not queried)"
		if d.ServiceState != nil {
			var err error
			state, err = d.ServiceState()
			if err != nil && !errors.Is(err, ks.ErrNotInstalled) {
				state = "Task Scheduler unavailable: " + err.Error()
			}
		}
		add("service", "info", state, "driga service status")
		add("hooks install", "info", "Claude/Codex/Copilot (preview before write)", "driga hooks install --dry-run")
		add("terminal jump", "info", "Windows Terminal on PATH; otherwise cmd.exe start (new window)", "")
		notifications := "native WinRT plain toast (not queried)"
		if d.NotificationStatus != nil {
			var err error
			notifications, err = d.NotificationStatus()
			if err != nil {
				notifications = "notifications unavailable: " + err.Error()
			}
		}
		add("notifications", "info", notifications, "check Windows notification settings; driga inbox")
	}
	if goos == "linux" {
		checks = append(checks, doctorLinuxChecks(d)...)
	}
	if goos == "darwin" {
		lookup := d.NotifierLookup
		if lookup == nil {
			lookup = inbox.TerminalNotifierPath
		}
		if lookup() != "" {
			add("notifications", "ok", "terminal-notifier (clickable)", "")
		} else {
			add("notifications", "info", "osascript (Script Editor)", "brew install terminal-notifier")
		}
	}
	checks = append(checks, doctorIntake(d, held))
	return checks
}

func doctorIntake(d DoctorDeps, held bool) Check {
	c := Check{Name: "otlp intake", Status: "info", Detail: "off (optional; driga hooks install --otlp)"}
	if !d.Config.Intake.OTLP {
		return c
	}
	c.Detail = "no OTLP events since start"
	if d.Store == nil {
		return c
	}
	read := func(key string) string { v, _ := d.Store.Meta("otlp:" + key); return v }
	count := read("received")
	if count != "" && count != "0" {
		c.Status = "ok"
		c.Detail = "events received: " + count
	}
	rejected := read("rejected")
	if rejected == "" {
		rejected = "0"
	}
	if rejected != "0" {
		c.Status = "warn"
		if count == "" || count == "0" {
			c.Detail = "no OTLP records accepted since start"
		}
		c.Fix = "check collector logs for otlp_request_rejected"
	}
	c.Detail += "; rejected: " + rejected
	var reasons []string
	for _, reason := range intake.RejectionReasons() {
		if n := read("reject:" + reason); n != "" && n != "0" {
			reasons = append(reasons, n+" "+reason)
		}
	}
	if len(reasons) > 0 {
		c.Detail += " (" + strings.Join(reasons, ", ") + ")"
	}
	if addr := read("listen"); addr != "" && held {
		c.Detail = "listening on " + addr + "; " + c.Detail
	}
	for _, kv := range [][2]string{{"parse_failures", "parse failures"}, {"ignored", "ignored"}, {"invalid_records", "invalid records"}, {"dropped", "content attributes dropped"}, {"unmatched", "unmatched OTLP sessions"}} {
		v := read(kv[0])
		if v == "" {
			v = "0"
		}
		c.Detail += "; " + kv[1] + ": " + v
	}
	if sources, err := d.Store.Sources(); err == nil {
		for _, s := range sources {
			if s.Name == "otlp" && s.LastError != "" && !s.LastErrorAt.Before(s.LastOKAt) {
				c.Status = "warn"
				c.Detail += "; last error: " + s.LastError
				c.Fix = "check intake.listen and restart the collector"
			}
		}
	}
	return c
}

func doctorLinuxChecks(d DoctorDeps) []Check {
	root := d.ProcRoot
	if root == "" {
		root = "/proc"
	}
	uid, username := d.UID, d.Username
	if username == "" {
		uid = os.Getuid()
		if u, err := user.Current(); err == nil {
			username = u.Username
		}
	}
	checks := []Check{}
	b, err := os.ReadFile(filepath.Join(root, "pressure/memory"))
	if errors.Is(err, os.ErrNotExist) {
		checks = append(checks, Check{"psi", "info", "psi n/a", ""})
	} else if err != nil {
		checks = append(checks, Check{"psi", "warn", err.Error(), "check access to /proc/pressure/memory"})
	} else if psi, err := platform.ParsePSI(b); err != nil || psi == nil {
		checks = append(checks, Check{"psi", "warn", "memory PSI unreadable", "check /proc/pressure/memory"})
	} else {
		checks = append(checks, Check{"psi", "ok", fmt.Sprintf("memory some avg10=%.2f", *psi), ""})
	}
	if pid, ok := platform.UserSubreaper(root, uid); ok {
		checks = append(checks, Check{"systemd_user", "ok", fmt.Sprintf("systemd --user pid %d", pid), ""})
	} else {
		checks = append(checks, Check{"systemd_user", "warn", "no systemd user instance found", "start a systemd user session or run driga collect in the foreground"})
	}
	linger := d.LingerDir
	if linger == "" {
		linger = "/var/lib/systemd/linger"
	}
	info, err := os.Stat(filepath.Join(linger, username))
	if username != "" && err == nil && info.Mode().IsRegular() {
		checks = append(checks, Check{"linger", "ok", "enabled; collector can run after logout", ""})
	} else {
		checks = append(checks, Check{"linger", "warn", "not enabled; collector may stop after logout", "loginctl enable-linger $USER"})
	}
	return checks
}

func isCollectorProc(p model.Proc) bool {
	if p.Exe != "" {
		if !strings.EqualFold(filepath.Base(p.Exe), "driga") && !strings.EqualFold(filepath.Base(p.Exe), "driga.exe") {
			return false
		}
		// Process APIs may join argv without quoting paths containing spaces.
		if rest, ok := strings.CutPrefix(p.Cmdline, p.Exe); ok && strings.HasPrefix(rest, " ") {
			args := strings.Fields(rest)
			return len(args) > 0 && args[0] == "collect"
		}
	}
	args, err := splitCommandWords(p.Cmdline)
	return err == nil && len(args) >= 2 && (filepath.Base(args[0]) == "driga" || strings.EqualFold(filepath.Base(args[0]), "driga.exe")) && args[1] == "collect"
}

// probeCollectorLock does not create a file or modify its contents/permissions.
func probeCollectorLock(dir string) (bool, error) {
	flags := os.O_RDONLY
	if runtime.GOOS == "windows" {
		flags = os.O_RDWR
	}
	f, err := os.OpenFile(filepath.Join(dir, "driga.lock"), flags, 0)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	err = filelock.Lock(f, true)
	if filelock.Busy(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, filelock.Unlock(f)
}

// runtimeDoctor gives CLI and MCP diagnostics the same read-only probes.
// Creating a service handle and querying its state never installs a task;
// notification status never registers an identity or displays an alert.
func runtimeDoctor(ctx context.Context, cfg config.Config, now time.Time, reader DoctorReader) []Check {
	d := DoctorDeps{DataDir: config.DataDir(), Config: cfg, Home: config.ExpandHome("~"), CodexHome: os.Getenv("CODEX_HOME"), CopilotHome: os.Getenv("COPILOT_HOME"), Now: now, Store: reader, LockProbe: probeCollectorLock}
	if runtime.GOOS == "windows" {
		d.NotificationStatus = inbox.WindowsNotificationStatus
		d.ServiceState = func() (string, error) {
			bin, err := os.Executable()
			if err != nil {
				return "", err
			}
			s, err := service.New(bin)
			if err != nil {
				return "", err
			}
			return s.(interface{ TaskState() (string, error) }).TaskState()
		}
	}
	return Doctor(ctx, d)
}

// DoctorCmd renders setup diagnostics and fails only for a failed check.
type DoctorCmd struct{}

// Run reads existing daemon data without falling back to a collection snapshot.
func (DoctorCmd) Run(g *Globals) error {
	cfg, err := config.Load(config.Path())
	if err != nil {
		return err
	}
	dataDir := config.DataDir()
	var reader DoctorReader
	st, openErr := store.Open(filepath.Join(dataDir, "driga.db"), store.ReadOnly)
	if openErr == nil {
		defer st.Close()
		reader = st
	}
	checks := runtimeDoctor(context.Background(), cfg, time.Now(), reader)
	if openErr != nil {
		status, fix := "fail", "check database permissions and driga service logs"
		if _, err := os.Stat(filepath.Join(dataDir, "driga.db")); errors.Is(err, os.ErrNotExist) {
			status, fix = "warn", "driga service install"
		}
		checks = append(checks, Check{"database", status, openErr.Error(), fix})
	}
	if g.JSON {
		var failed []string
		for _, check := range checks {
			if check.Status == "fail" {
				failed = append(failed, check.Name+": "+check.Detail+"; "+check.Fix)
			}
		}
		if len(failed) > 0 {
			return fmt.Errorf("doctor found failed checks: %s", strings.Join(failed, "; "))
		}
		err = json.NewEncoder(commandOutput(g)).Encode(checks)
	} else {
		w := tabwriter.NewWriter(commandOutput(g), 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "CHECK\tSTATUS\tDETAIL\tFIX")
		for _, check := range checks {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", strings.ReplaceAll(check.Name, "_", "-"), check.Status, check.Detail, check.Fix)
		}
		err = w.Flush()
	}
	if err != nil {
		return err
	}
	for _, check := range checks {
		if check.Status == "fail" {
			return errors.New("doctor found failed checks")
		}
	}
	return nil
}
