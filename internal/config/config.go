// Package config loads driga's TOML config and resolves its data, spool and
// config paths. Every path is overridable by environment for tests.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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
		Adapters: map[string]AdapterConfig{
			"claude": {Enabled: true},
			"codex":  {Enabled: true},
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
func DataDir() string {
	if p := os.Getenv("DRIGA_DATA_DIR"); p != "" {
		return p
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
