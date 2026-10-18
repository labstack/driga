package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/hooks"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
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
	DataDir   string
	Store     DoctorReader
	Config    config.Config
	Home      string
	CodexHome string // Optional override; defaults to Home/.codex.
	Now       time.Time
	LockProbe func(dataDir string) (held bool, err error)
}

// Doctor checks stored evidence without collecting, writing, or controlling launchd.
func Doctor(ctx context.Context, d DoctorDeps) []Check {
	checks := []Check{}
	add := func(name, status, detail, fix string) { checks = append(checks, Check{name, status, detail, fix}) }
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
			interval := intervals.Procs
			switch source.Name {
			case "git":
				interval = intervals.Git
			case "machine":
				interval = intervals.Machine
			case "cwds":
				interval = intervals.Cwds
			}
			age := d.Now.Sub(source.LastOKAt)
			status, fix := "ok", ""
			detail := "last success " + age.Round(time.Second).String() + " ago"
			if source.LastOKAt.IsZero() {
				detail = "no successful collection"
			}
			if source.LastOKAt.IsZero() || age < 0 || age > 3*interval || source.LastErrorAt.After(source.LastOKAt) {
				status, fix = "warn", "driga service restart"
			}
			if source.LastError != "" {
				detail += "; last error: " + source.LastError
			}
			add("source:"+source.Name, status, detail, fix)
		}
		procs, err := d.Store.Procs()
		if err != nil {
			add("collector-rss", "fail", err.Error(), "check database access; run driga service restart")
		} else {
			var rss uint64
			found := false
			for _, p := range procs {
				if isCollectorProc(p) {
					rss += p.RSS
					found = true
				}
			}
			status, fix, detail := "ok", "", fmt.Sprintf("%.1f MB", float64(rss)/1e6)
			if !found {
				status, detail, fix = "warn", "collector absent from process snapshot", "driga service start"
			} else if rss > 50_000_000 {
				status, fix = "warn", "driga service restart; inspect driga service logs"
			}
			add("collector-rss", status, detail, fix)
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
		switch name {
		case "claude":
			dirs = []string{filepath.Join(d.Home, ".claude", "sessions"), filepath.Join(d.Home, ".claude", "projects")}
		case "codex":
			dirs = []string{filepath.Join(codexHome, "sessions")}
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
			warning, err := d.Store.Meta("warn:" + name + ":untested-version")
			if err == nil && warning != "" {
				add("version:"+name, "warn", "untested version; tolerant parsing: "+warning, "update driga to a release tested with this agent version")
			} else if err != nil && !errors.Is(err, store.ErrNotFound) {
				add("version:"+name, "fail", err.Error(), "check database access")
			}
		}
		switch name {
		case "claude":
			events, err := hooks.ClaudeStatus(filepath.Join(d.Home, ".claude", "settings.json"))
			if err != nil {
				add("hooks:claude", "fail", err.Error(), "driga hooks install --agent claude")
			} else if len(events) < 4 {
				add("hooks:claude", "warn", "optional hooks incomplete or absent", "driga hooks install --agent claude")
			} else {
				add("hooks:claude", "ok", strings.Join(events, ", "), "")
			}
		case "codex":
			diff, err := hooks.CodexUninstall(filepath.Join(codexHome, "config.toml"), true)
			if err != nil {
				add("hooks:codex", "fail", err.Error(), "driga hooks install --agent codex")
			} else if !diff.Changed {
				add("hooks:codex", "warn", "optional notify hook absent; rollout tailing remains available", "driga hooks install --agent codex")
			} else {
				add("hooks:codex", "ok", "notify installed", "")
			}
		}
	}
	info, err := os.Stat(d.DataDir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		add("data-permissions", "warn", "data directory does not exist", "driga service install")
	case err != nil:
		add("data-permissions", "fail", err.Error(), "check access to "+d.DataDir)
	case !info.IsDir():
		add("data-permissions", "fail", "data path is not a directory", "set DRIGA_DATA_DIR to a private directory")
	case info.Mode().Perm() != 0700:
		add("data-permissions", "fail", fmt.Sprintf("directory permissions are %04o; expected 0700", info.Mode().Perm()), "chmod 700 "+d.DataDir)
	default:
		add("data-permissions", "ok", "0700", "")
	}
	return checks
}

func isCollectorProc(p model.Proc) bool {
	if p.Exe != "" {
		if filepath.Base(p.Exe) != "driga" {
			return false
		}
		// Process APIs may join argv without quoting paths containing spaces.
		if rest, ok := strings.CutPrefix(p.Cmdline, p.Exe); ok && strings.HasPrefix(rest, " ") {
			args := strings.Fields(rest)
			return len(args) > 0 && args[0] == "collect"
		}
	}
	args, err := splitCommandWords(p.Cmdline)
	return err == nil && len(args) >= 2 && filepath.Base(args[0]) == "driga" && args[1] == "collect"
}

// probeCollectorLock does not create a file or modify its contents/permissions.
func probeCollectorLock(dir string) (bool, error) {
	f, err := os.Open(filepath.Join(dir, "driga.lock"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}

// DoctorCmd renders setup diagnostics and fails only for a failed check.
type DoctorCmd struct{}

// Run reads existing daemon data without falling back to a collection snapshot.
func (DoctorCmd) Run(g *Globals) error {
	cfg, err := config.Load(config.Path())
	if err != nil {
		return err
	}
	d := DoctorDeps{DataDir: config.DataDir(), Config: cfg, Home: config.ExpandHome("~"), CodexHome: os.Getenv("CODEX_HOME"), Now: time.Now(), LockProbe: probeCollectorLock}
	st, openErr := store.Open(filepath.Join(d.DataDir, "driga.db"), store.ReadOnly)
	if openErr == nil {
		defer st.Close()
		d.Store = st
	}
	checks := Doctor(context.Background(), d)
	if openErr != nil {
		status, fix := "fail", "check database permissions and driga service logs"
		if _, err := os.Stat(filepath.Join(d.DataDir, "driga.db")); errors.Is(err, os.ErrNotExist) {
			status, fix = "warn", "driga service install"
		}
		checks = append(checks, Check{"database", status, openErr.Error(), fix})
	}
	if g.JSON {
		err = json.NewEncoder(commandOutput(g)).Encode(checks)
	} else {
		w := tabwriter.NewWriter(commandOutput(g), 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "CHECK\tSTATUS\tDETAIL\tFIX")
		for _, check := range checks {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", check.Name, check.Status, check.Detail, check.Fix)
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
