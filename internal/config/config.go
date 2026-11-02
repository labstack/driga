// Package config loads driga's TOML config and resolves its data, spool and
// config paths. Every path is overridable by environment for tests.
package config

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/labstack/driga/internal/variant"
)

// Config is the effective configuration (§12 of the design).
type Config struct {
	Warnings        []string
	UI              UIConfig
	App             AppConfig
	Motion          string
	Roots           []string
	RootDepth       int
	Exclude         []string
	StuckAfter      time.Duration
	OrphanMinAge    time.Duration
	CaptureText     string
	OpenCmd         string
	NoServiceHint   bool
	Adapters        map[string]AdapterConfig
	ExtraSignatures []string
	Fleet           FleetConfig
	Intake          IntakeConfig
	Summaries       SummariesConfig
	Machine         MachineConfig
	Cleanup         CleanupConfig
	Inbox           InboxConfig
}

type UIConfig struct{ Theme string }

// AppConfig controls the desktop app.
type AppConfig struct {
	Tray          bool
	LaunchAtLogin bool
}

// MachineConfig controls machine warnings; zero selects the documented default.
type MachineConfig struct{ DiskFreeMin uint64 }

type InboxConfig struct {
	Alerts       string
	AlertAfter   time.Duration
	AlertRepeat  time.Duration
	JumpTerminal bool
}

// CleanupConfig bounds eligibility and regenerable ignored directories.
type CleanupConfig struct {
	WorktreeIdle time.Duration
	Regenerable  []string
}

type SummariesConfig struct {
	Enabled bool
	Model   string
}

// FleetConfig controls optional fleet serving and text sharing.
type FleetConfig struct {
	Serve           bool
	Listen          string
	ShareText       string
	EnrollWindow    time.Duration
	AllowAnyAddress bool
}

// IntakeConfig controls the optional loopback OTLP/HTTP JSON listener.
type IntakeConfig struct {
	OTLP   bool
	Listen string
}

// AdapterConfig toggles one agent adapter.
type AdapterConfig struct {
	Enabled bool
}

// CaptureSnippets reports whether prompt/handoff snippets may be stored.
func (c Config) CaptureSnippets() bool { return c.CaptureText == "snippets" }

func home() string {
	if runtime.GOOS == "windows" {
		h, _ := os.UserHomeDir()
		return h
	}
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

// ExpandHome replaces a leading "~" with $HOME.
func ExpandHome(p string) string {
	if p == "~" {
		return home()
	}
	if strings.HasPrefix(p, "~/") || runtime.GOOS == "windows" && strings.HasPrefix(p, `~\`) {
		return filepath.Join(home(), p[2:])
	}
	return p
}

// Path is the config file location (env DRIGA_CONFIG overrides).
func Path() string { return absolutePath(configPath()) }

func configPath() string {
	if p := os.Getenv("DRIGA_CONFIG"); p != "" {
		return p
	}
	return defaultConfigPath()
}

func defaultConfigPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(windowsAppDir("APPDATA", "Roaming"), "driga", "config.toml")
	}
	if p := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(p) {
		return filepath.Join(p, "driga", "config.toml")
	}
	return filepath.Join(home(), ".config", "driga", "config.toml")
}

// DataDir holds driga.db, driga.lock and viewer.heartbeat (env DRIGA_DATA_DIR overrides).
func DataDir() string { return absolutePath(dataDirForOS(runtime.GOOS)) }

// DefaultDataDir is the data directory when DRIGA_DATA_DIR is unset.
func DefaultDataDir() string { return absolutePath(defaultDataDirForOS(runtime.GOOS)) }

func dataDirForOS(goos string) string {
	if p := os.Getenv("DRIGA_DATA_DIR"); p != "" {
		return p
	}
	return defaultDataDirForOS(goos)
}

func defaultDataDirForOS(goos string) string { return dataDirNamed(goos, dataName()) }

func dataDirNamed(goos, name string) string {
	if goos == "windows" {
		return filepath.Join(windowsAppDir("LOCALAPPDATA", "Local"), name)
	}
	if goos == "linux" {
		if p := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(p) {
			return filepath.Join(p, name)
		}
		return filepath.Join(home(), ".local", "share", name)
	}
	return filepath.Join(home(), "Library", "Application Support", name)
}

// dataName names the default data and spool directories: "driga" for a
// release and "driga-dev" for a development build, so a development service
// never shares the release's database or spool. The configuration file is
// shared.
func dataName() string { return "driga" + variant.Suffix() }

// SpoolDir holds hook/ack event files (env DRIGA_SPOOL_DIR overrides).
func SpoolDir() string { return absolutePath(spoolDirForOS(runtime.GOOS)) }

func spoolDirForOS(goos string) string {
	if p := os.Getenv("DRIGA_SPOOL_DIR"); p != "" {
		return p
	}
	return spoolDirNamed(goos, dataName())
}

func spoolDirNamed(goos, name string) string {
	if goos == "linux" {
		if p := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(p) {
			return filepath.Join(p, name, "events")
		}
		return filepath.Join(home(), ".local", "state", name, "events")
	}
	return filepath.Join(dataDirNamed(goos, name), "events")
}

// StateDirs lists every file and directory where any Driga build keeps
// state: the configuration, this process's data and spool directories with
// their overrides, and the default data and state directories of a release,
// a development build and every driga-<name> variant found beside them.
// Commands Driga runs for an agent are denied all of them.
func StateDirs() []string { return stateDirsForOS(runtime.GOOS) }

func stateDirsForOS(goos string) []string {
	dirs := []string{absolutePath(configPath()), absolutePath(filepath.Dir(defaultConfigPath())), absolutePath(dataDirForOS(goos)), absolutePath(spoolDirForOS(goos))}
	names := []string{"driga", "driga-dev"}
	for _, root := range []string{filepath.Dir(dataDirNamed(goos, "driga")), filepath.Dir(filepath.Dir(spoolDirNamed(goos, "driga")))} {
		matches, _ := filepath.Glob(filepath.Join(root, "driga-*"))
		for _, match := range matches {
			names = append(names, filepath.Base(match))
		}
	}
	for _, name := range names {
		// The spool's parent is the data directory, or on Linux the state
		// directory that also holds the service log.
		dirs = append(dirs, absolutePath(dataDirNamed(goos, name)), absolutePath(filepath.Dir(spoolDirNamed(goos, name))))
	}
	slices.Sort(dirs)
	return slices.Compact(dirs)
}

func windowsAppDir(env, leaf string) string {
	if p := os.Getenv(env); p != "" {
		return p
	}
	return filepath.Join(home(), "AppData", leaf)
}

func absolutePath(p string) string {
	if abs, err := filepath.Abs(ExpandHome(p)); err == nil {
		return abs
	}
	return filepath.Clean(ExpandHome(p))
}

// ResolvePaths freezes data/spool paths and explicit config overrides at startup,
// before any worker or child process can change directories. The platform default
// config remains optional; setting DRIGA_CONFIG would make it a required override.
func ResolvePaths() error {
	for _, entry := range []struct{ key, path string }{
		{"DRIGA_DATA_DIR", DataDir()},
		{"DRIGA_SPOOL_DIR", SpoolDir()},
		{"DRIGA_CONFIG", Path()},
	} {
		if entry.key == "DRIGA_CONFIG" && os.Getenv(entry.key) == "" {
			continue
		}
		p, err := filepath.Abs(entry.path)
		if err != nil {
			return err
		}
		if err := os.Setenv(entry.key, p); err != nil {
			return err
		}
	}
	return nil
}
