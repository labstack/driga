// Package config loads driga's TOML config and resolves its data, spool and
// config paths. Every path is overridable by environment for tests.
package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Config is the effective configuration (§12 of the design).
type Config struct {
	Motion          string
	Roots           []string
	RootDepth       int
	Exclude         []string
	StalledAfter    time.Duration
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
	WorktreeIdle   time.Duration
	LowDiskPercent float64
	LowDiskBytes   uint64
	Regenerable    []string
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
func Path() string {
	if p := os.Getenv("DRIGA_CONFIG"); p != "" {
		return p
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(windowsAppDir("APPDATA", "Roaming"), "driga", "config.toml")
	}
	if p := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(p) {
		return filepath.Join(p, "driga", "config.toml")
	}
	return filepath.Join(home(), ".config", "driga", "config.toml")
}

// DataDir holds driga.db, driga.lock and viewer.heartbeat (env DRIGA_DATA_DIR overrides).
func DataDir() string { return dataDirForOS(runtime.GOOS) }

func dataDirForOS(goos string) string {
	if p := os.Getenv("DRIGA_DATA_DIR"); p != "" {
		return p
	}
	if goos == "windows" {
		return filepath.Join(windowsAppDir("LOCALAPPDATA", "Local"), "driga")
	}
	if goos == "linux" {
		if p := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(p) {
			return filepath.Join(p, "driga")
		}
		return filepath.Join(home(), ".local", "share", "driga")
	}
	return filepath.Join(home(), "Library", "Application Support", "driga")
}

// SpoolDir holds hook/ack event files (env DRIGA_SPOOL_DIR overrides).
func SpoolDir() string { return spoolDirForOS(runtime.GOOS) }

func spoolDirForOS(goos string) string {
	if p := os.Getenv("DRIGA_SPOOL_DIR"); p != "" {
		return p
	}
	if goos == "windows" {
		return filepath.Join(windowsAppDir("LOCALAPPDATA", "Local"), "driga", "events")
	}
	if p := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(p) {
		return filepath.Join(p, "driga", "events")
	}
	if goos == "linux" {
		return filepath.Join(home(), ".local", "state", "driga", "events")
	}
	return filepath.Join(home(), "Library", "Application Support", "driga", "events")
}

// LegacySpoolDir is only read by the collector migration.
func LegacySpoolDir() string { return filepath.Join(home(), ".driga", "events") }

func windowsAppDir(env, leaf string) string {
	if p := os.Getenv(env); p != "" {
		return p
	}
	return filepath.Join(home(), "AppData", leaf)
}
