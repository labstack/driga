// Package config loads driga's TOML config and resolves its data, spool and
// config paths. Every path is overridable by environment for tests.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// Config is the effective configuration (§12 of the design).
type Config struct {
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
	Cleanup         CleanupConfig
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

// Default returns the built-in defaults. No roots: repos come from session cwds.
func Default() Config {
	return Config{
		RootDepth:    3,
		Exclude:      []string{"**/node_modules", "**/.tmp/**"},
		StuckAfter:   10 * time.Minute,
		OrphanMinAge: 10 * time.Minute,
		CaptureText:  "titles",
		Summaries:    SummariesConfig{Model: "haiku"},
		Cleanup:      CleanupConfig{WorktreeIdle: 168 * time.Hour, LowDiskPercent: 10, LowDiskBytes: 10 << 30, Regenerable: []string{"node_modules/", "target/", ".venv/", "venv/", "dist/", "build/", ".next/", "__pycache__/", ".gradle/", ".terraform/"}},
		Fleet:        FleetConfig{ShareText: "titles", EnrollWindow: 10 * time.Minute},
		Intake:       IntakeConfig{Listen: "127.0.0.1:7418"},
		Adapters: map[string]AdapterConfig{
			"claude": {Enabled: true},
			"codex":  {Enabled: true},
			"gemini": {Enabled: true},
		},
	}
}

type duration struct{ d time.Duration }

func (d *duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	d.d = v
	return nil
}

type fileAdapter struct {
	Enabled *bool `toml:"enabled"`
}

type file struct {
	Cleanup struct {
		WorktreeIdle   *duration `toml:"worktree_idle"`
		LowDiskPercent *float64  `toml:"low_disk_percent"`
		LowDiskBytes   *string   `toml:"low_disk_bytes"`
		Regenerable    []string  `toml:"regenerable"`
	} `toml:"cleanup"`
	Summaries struct {
		Enabled *bool   `toml:"enabled"`
		Model   *string `toml:"model"`
	} `toml:"summaries"`
	Roots         []string               `toml:"roots"`
	RootDepth     *int                   `toml:"root_depth"`
	Exclude       []string               `toml:"exclude"`
	StuckAfter    *duration              `toml:"stuck_after"`
	OrphanMinAge  *duration              `toml:"orphan_min_age"`
	CaptureText   *string                `toml:"capture_text"`
	OpenCmd       *string                `toml:"open_cmd"`
	NoServiceHint *bool                  `toml:"no_service_hint"`
	Adapters      map[string]fileAdapter `toml:"adapters"`
	Orphans       struct {
		ExtraSignatures []string `toml:"extra_signatures"`
	} `toml:"orphans"`
	Fleet struct {
		Serve           *bool     `toml:"serve"`
		Listen          *string   `toml:"listen"`
		ShareText       *string   `toml:"share_text"`
		EnrollWindow    *duration `toml:"enroll_window"`
		AllowAnyAddress *bool     `toml:"allow_any_address"`
	} `toml:"fleet"`
	Intake struct {
		OTLP   *bool   `toml:"otlp"`
		Listen *string `toml:"listen"`
	} `toml:"intake"`
}

// Load reads path; a missing file yields Default() and no error.
func Load(path string) (Config, error) {
	c := Default()
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	var f file
	if err := toml.Unmarshal(b, &f); err != nil {
		return c, fmt.Errorf("config %s: %w", path, err)
	}
	if f.Roots != nil {
		c.Roots = make([]string, len(f.Roots))
		for i, r := range f.Roots {
			c.Roots[i] = ExpandHome(r)
		}
	}
	if f.Cleanup.WorktreeIdle != nil {
		if f.Cleanup.WorktreeIdle.d < 0 {
			return c, fmt.Errorf("cleanup.worktree_idle must be non-negative")
		}
		c.Cleanup.WorktreeIdle = f.Cleanup.WorktreeIdle.d
	}
	if f.Cleanup.LowDiskPercent != nil {
		v := *f.Cleanup.LowDiskPercent
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 100 {
			return c, fmt.Errorf("cleanup.low_disk_percent must be between 0 and 100")
		}
		c.Cleanup.LowDiskPercent = v
	}
	if f.Cleanup.LowDiskBytes != nil {
		v := *f.Cleanup.LowDiskBytes
		mult := uint64(1)
		for suffix, n := range map[string]uint64{"GiB": 1 << 30, "MiB": 1 << 20, "KiB": 1 << 10} {
			if strings.HasSuffix(v, suffix) {
				v = strings.TrimSpace(strings.TrimSuffix(v, suffix))
				mult = n
				break
			}
		}
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil || n > ^uint64(0)/mult {
			return c, fmt.Errorf("invalid cleanup.low_disk_bytes")
		}
		c.Cleanup.LowDiskBytes = n * mult
	}
	if f.Cleanup.Regenerable != nil {
		seen := map[string]bool{}
		for _, p := range c.Cleanup.Regenerable {
			seen[p] = true
		}
		for _, p := range f.Cleanup.Regenerable {
			if p == "" || filepath.IsAbs(p) || strings.Contains(p, "..") || strings.Trim(p, "/") == "" {
				return c, fmt.Errorf("invalid cleanup.regenerable path %q", p)
			}
			if !seen[p] {
				c.Cleanup.Regenerable = append(c.Cleanup.Regenerable, p)
				seen[p] = true
			}
		}
	}
	if f.Summaries.Enabled != nil {
		c.Summaries.Enabled = *f.Summaries.Enabled
	}
	if f.Summaries.Model != nil {
		if strings.TrimSpace(*f.Summaries.Model) == "" {
			return c, fmt.Errorf("config %s: summaries.model must not be empty", path)
		}
		c.Summaries.Model = *f.Summaries.Model
	}
	if f.RootDepth != nil {
		if *f.RootDepth < 0 {
			return c, fmt.Errorf("config %s: root_depth must be >= 0", path)
		}
		c.RootDepth = *f.RootDepth
	}
	if f.Exclude != nil {
		c.Exclude = f.Exclude
	}
	if f.StuckAfter != nil {
		c.StuckAfter = f.StuckAfter.d
	}
	if f.OrphanMinAge != nil {
		c.OrphanMinAge = f.OrphanMinAge.d
	}
	if f.CaptureText != nil {
		switch *f.CaptureText {
		case "titles", "snippets":
			c.CaptureText = *f.CaptureText
		default:
			return c, fmt.Errorf("config %s: capture_text must be \"titles\" or \"snippets\", got %q", path, *f.CaptureText)
		}
	}
	if f.OpenCmd != nil {
		c.OpenCmd = *f.OpenCmd
	}
	if f.NoServiceHint != nil {
		c.NoServiceHint = *f.NoServiceHint
	}
	for name, a := range f.Adapters {
		ac := c.Adapters[name]
		if a.Enabled != nil {
			ac.Enabled = *a.Enabled
		}
		c.Adapters[name] = ac
	}
	for _, s := range f.Orphans.ExtraSignatures {
		c.ExtraSignatures = append(c.ExtraSignatures, ExpandHome(s))
	}
	if f.Fleet.Serve != nil {
		c.Fleet.Serve = *f.Fleet.Serve
	}
	if f.Fleet.Listen != nil {
		c.Fleet.Listen = *f.Fleet.Listen
	}
	if c.Fleet.Serve && strings.TrimSpace(c.Fleet.Listen) == "" {
		return c, fmt.Errorf("config %s: fleet.listen is required when fleet.serve is true", path)
	}
	if f.Fleet.ShareText != nil {
		switch *f.Fleet.ShareText {
		case "none", "titles", "snippets":
			c.Fleet.ShareText = *f.Fleet.ShareText
		default:
			return c, fmt.Errorf("config %s: fleet.share_text must be \"none\", \"titles\" or \"snippets\"", path)
		}
	}
	if f.Fleet.EnrollWindow != nil {
		if f.Fleet.EnrollWindow.d <= 0 {
			return c, fmt.Errorf("config %s: fleet.enroll_window must be positive", path)
		}
		c.Fleet.EnrollWindow = f.Fleet.EnrollWindow.d
	}
	if f.Fleet.AllowAnyAddress != nil {
		c.Fleet.AllowAnyAddress = *f.Fleet.AllowAnyAddress
	}
	if f.Intake.OTLP != nil {
		c.Intake.OTLP = *f.Intake.OTLP
	}
	if f.Intake.Listen != nil {
		c.Intake.Listen = *f.Intake.Listen
	}
	host, port, err := net.SplitHostPort(c.Intake.Listen)
	ip := net.ParseIP(host)
	p, portErr := strconv.Atoi(port)
	if err != nil || ip == nil || !ip.IsLoopback() || portErr != nil || p < 0 || p > 65535 {
		return c, fmt.Errorf("config %s: intake.listen requires a loopback IP and a numeric TCP port", path)
	}
	return c, nil
}

func home() string {
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
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home(), p[2:])
	}
	return p
}

// Path is the config file location (env DRIGA_CONFIG overrides).
func Path() string {
	if p := os.Getenv("DRIGA_CONFIG"); p != "" {
		return p
	}
	return filepath.Join(home(), ".config", "driga", "config.toml")
}

// DataDir holds driga.db, driga.lock and viewer.heartbeat (env DRIGA_DATA_DIR overrides).
func DataDir() string { return dataDirForOS(runtime.GOOS) }

func dataDirForOS(goos string) string {
	if p := os.Getenv("DRIGA_DATA_DIR"); p != "" {
		return p
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
func SpoolDir() string {
	if p := os.Getenv("DRIGA_SPOOL_DIR"); p != "" {
		return p
	}
	return filepath.Join(home(), ".driga", "events")
}
