package config

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/pelletier/go-toml/v2"
)

//go:generate go run ./genenv

// Setting is the authoritative consumer contract. Lists are file-only; all
// scalar names derive mechanically from Key. No dotenv files are loaded.
type Setting struct {
	Key, Field, Type string
	Default          any
	Purpose, Range   string
}

var settings = []Setting{
	{"motion", "Motion", "string", "on", "Event-bound motion in the terminal Flow map.", "on or off"},
	{"roots", "Roots", "list", []string{}, "Additional repository discovery roots; ~ expands to HOME.", "file-only; empty = session working directories only"},
	{"root_depth", "RootDepth", "int", int64(3), "Repository discovery depth.", "0..max int; zero = roots only"},
	{"exclude", "Exclude", "list", []string{"**/node_modules", "**/.tmp/**"}, "Repository discovery exclusions.", "file-only; empty = no exclusions"},
	{"stalled_after", "StalledAfter", "duration", "10m", "Quiet busy-session threshold.", "0..2562047h47m16.854775807s; zero = default 10m"},
	{"orphan_min_age", "OrphanMinAge", "duration", "10m", "Minimum orphan candidate age.", "0..2562047h47m16.854775807s; zero = no age minimum"},
	{"capture_text", "CaptureText", "string", "titles", "Local text capture policy.", "titles or snippets"},
	{"open_cmd", "OpenCmd", "string", "", "Editor command; {cwd} is substituted.", "empty = auto-detect"},
	{"no_service_hint", "NoServiceHint", "bool", false, "Suppress optional service hints.", "true or false"},
	{"ui.theme", "UI.Theme", "string", "auto", "Palette for the whole terminal UI.", "auto, mocha or latte"},
	{"adapters.claude.enabled", "", "bool", true, "Enable Claude session discovery.", "true or false"},
	{"adapters.codex.enabled", "", "bool", true, "Enable Codex session discovery.", "true or false"},
	{"adapters.gemini.enabled", "", "bool", true, "Enable Gemini session discovery.", "true or false"},
	{"adapters.copilot.enabled", "", "bool", true, "Enable Copilot hook and process discovery.", "true or false"},
	{"orphans.extra_signatures", "ExtraSignatures", "list", []string{}, "Additional signature files; ~ expands to HOME.", "file-only; empty = built-in signatures"},
	{"machine.disk_free_min", "Machine.DiskFreeMin", "size", "0", "Low disk free-space limit. Zero uses the smaller of 10% of the volume or 20 GB (decimal). Replaces cleanup.low_disk_* warning thresholds.", "0..18446744073709551615 bytes; optional KiB/MiB/GiB suffix; zero = automatic"},
	{"summaries.enabled", "Summaries.Enabled", "bool", false, "Enable optional local Claude summaries.", "true or false"},
	{"summaries.model", "Summaries.Model", "string", "haiku", "Model selection passed to the Claude CLI.", "nonempty"},
	{"cleanup.worktree_idle", "Cleanup.WorktreeIdle", "duration", "168h", "Idle worktree cleanup eligibility.", "0..2562047h47m16.854775807s; zero = no idle minimum"},
	{"cleanup.regenerable", "Cleanup.Regenerable", "list", []string{"node_modules/", "target/", ".venv/", "venv/", "dist/", "build/", ".next/", "__pycache__/", ".gradle/", ".terraform/"}, "Additional regenerable relative directories (merged with defaults).", "file-only; empty = built-in directories"},
	{"fleet.serve", "Fleet.Serve", "bool", false, "Enable optional fleet serving.", "true or false; requires fleet.listen"},
	{"fleet.listen", "Fleet.Listen", "string", "", "Fleet TCP listener address.", "required with fleet.serve; explicit private IP/interface, public addresses require allow_any_address"},
	{"fleet.share_text", "Fleet.ShareText", "string", "titles", "Fleet export text policy, bounded by local capture policy.", "none, titles or snippets"},
	{"fleet.enroll_window", "Fleet.EnrollWindow", "duration", "10m", "Fleet enrollment duration.", "1ns..10m; zero rejected"},
	{"fleet.allow_any_address", "Fleet.AllowAnyAddress", "bool", false, "Permit explicit non-loopback fleet listener.", "true or false"},
	{"inbox.alerts", "Inbox.Alerts", "string", "waiting", "Desktop alerts for attention and optionally completions.", "waiting, all or off"},
	{"inbox.alert_after", "Inbox.AlertAfter", "duration", "0s", "Alert only when a wait is still open this long after it began.", "0..2562047h47m16.854775807s; zero = alert immediately"},
	{"inbox.alert_repeat", "Inbox.AlertRepeat", "duration", "5m", "Minimum interval between alerts for one session.", "0..2562047h47m16.854775807s; zero = no throttle"},
	{"inbox.jump_terminal", "Inbox.JumpTerminal", "bool", false, "Opt in to bringing a proven macOS terminal window to front.", "true or false"},
	{"intake.otlp", "Intake.OTLP", "bool", false, "Enable optional loopback OTLP/HTTP JSON intake.", "true or false"},
	{"intake.listen", "Intake.Listen", "string", "127.0.0.1:7418", "OTLP intake TCP listener address.", "loopback IP and port 0..65535; zero = ephemeral port"},
}

func (s Setting) EnvName() string {
	if s.Type == "list" {
		return ""
	}
	return "DRIGA_" + strings.ToUpper(strings.NewReplacer(".", "_", "-", "_").Replace(s.Key))
}

// Settings returns the documented setting inventory, in stable display order.
func Settings() []Setting { return append([]Setting(nil), settings...) }

// Value returns the effective native value for config rendering.
func (s Setting) Value(c Config) any {
	if strings.HasPrefix(s.Key, "adapters.") {
		return c.Adapters[strings.Split(s.Key, ".")[1]].Enabled
	}
	return s.field(&c).Interface()
}

// runtimeSettings documents the explicit namespace extensions. No wildcard
// exemptions are accepted. Internal names are inherited by helper/test processes.
var runtimeSettings = []struct{ Name, Type, Default, Purpose string }{
	{"DRIGA_CONFIG", "path", "$XDG_CONFIG_HOME/driga/config.toml or ~/.config/driga/config.toml", "Configuration file override."},
	{"DRIGA_DATA_DIR", "path", "Linux: $XDG_DATA_HOME/driga or ~/.local/share/driga; macOS: ~/Library/Application Support/driga", "Database and fleet data directory override."},
	{"DRIGA_SPOOL_DIR", "path", "Linux: $XDG_STATE_HOME/driga/events or ~/.local/state/driga/events; macOS: ~/Library/Application Support/driga/events", "Hook/ack spool directory override."},
	{"DRIGA_CHAT_SOCKET", "path", "unset", "Chat broker socket for the MCP child of one Chat turn."},
	{"DRIGA_CHAT_CONVERSATION", "id", "unset", "Chat conversation of the MCP child's turn."},
	{"DRIGA_CHAT_TOKEN", "secret", "unset", "Per-turn token tying the MCP child's approval requests to its turn."},
	{"DRIGA_CHAT_READ_ONLY", "marker", "unset", "1 keeps the Chat MCP child read-only."},
	{"DRIGA_INTERNAL", "marker", "unset", "1 suppresses recursive hooks from Driga-owned Claude subprocesses."},
	{"DRIGA_FIXTURE_OUTPUT", "path", "unset", "Fixture generator output directory (tests only)."},
	{"DRIGA_COLLECTOR_HEAP_CHILD", "marker", "unset", "1 selects isolated collector heap-test child."},
	{"DRIGA_FLEET_HEAP_CHILD", "marker", "unset", "1 selects isolated fleet heap-test child."},
	{"DRIGA_OTLP_TRACES_ADDED", "marker", "unset", "1 records enhanced telemetry installed by Driga in Claude settings."},
	{"DRIGA_JSON_WATCH_CHILD", "marker", "unset", "1 selects isolated NDJSON watch-test child."},
	{"DRIGA_TEST_MCP_PROCESS", "marker", "unset", "1 selects isolated MCP-test child."},
}

func Default() Config {
	c := Config{Adapters: map[string]AdapterConfig{}}
	for _, s := range settings {
		if err := s.assign(&c, s.Default); err != nil {
			panic(fmt.Sprintf("invalid default %s: %v", s.Key, err))
		}
	}
	return c
}

func (s Setting) field(c *Config) reflect.Value {
	v := reflect.ValueOf(c).Elem()
	for _, name := range strings.Split(s.Field, ".") {
		v = v.FieldByName(name)
	}
	return v
}

func (s Setting) assign(c *Config, raw any) error {
	var v any
	switch s.Type {
	case "duration":
		text, ok := raw.(string)
		if !ok {
			return fmt.Errorf("expected duration string; allowed range %s", s.Range)
		}
		d, err := time.ParseDuration(text)
		if err != nil || d < 0 || (s.Key == "fleet.enroll_window" && (d == 0 || d > 10*time.Minute)) {
			return fmt.Errorf("expected Go duration; allowed range %s", s.Range)
		}
		if s.Key == "stalled_after" && d == 0 {
			d = 10 * time.Minute
		}
		v = d
	case "size":
		text, ok := raw.(string)
		if !ok {
			return fmt.Errorf("expected size string; allowed range %s", s.Range)
		}
		mult := uint64(1)
		for _, unit := range []struct {
			suffix string
			n      uint64
		}{{"KiB", 1 << 10}, {"MiB", 1 << 20}, {"GiB", 1 << 30}} {
			if strings.HasSuffix(text, unit.suffix) {
				text = strings.TrimSpace(strings.TrimSuffix(text, unit.suffix))
				mult = unit.n
				break
			}
		}
		n, err := strconv.ParseUint(text, 10, 64)
		if err != nil || n > math.MaxUint64/mult {
			return fmt.Errorf("expected integer bytes or KiB/MiB/GiB; allowed range %s", s.Range)
		}
		v = n * mult
	case "int":
		n, ok := raw.(int64)
		if !ok || n < 0 || uint64(n) > uint64(^uint(0)>>1) {
			return fmt.Errorf("expected integer; allowed range %s", s.Range)
		}
		v = int(n)
	case "bool":
		b, ok := raw.(bool)
		if !ok {
			return fmt.Errorf("expected true or false")
		}
		v = b
	case "string":
		text, ok := raw.(string)
		if !ok {
			return fmt.Errorf("expected string")
		}
		switch s.Key {
		case "ui.theme":
			if text != "auto" && text != "mocha" && text != "latte" {
				return fmt.Errorf("allowed values: %s", s.Range)
			}
		case "motion":
			if text != "on" && text != "off" {
				return fmt.Errorf("allowed values: %s", s.Range)
			}
		case "inbox.alerts":
			if text != "waiting" && text != "all" && text != "off" {
				return fmt.Errorf("allowed values: %s", s.Range)
			}
		case "capture_text":
			if text != "titles" && text != "snippets" {
				return fmt.Errorf("allowed values: %s", s.Range)
			}
		case "fleet.share_text":
			if text != "none" && text != "titles" && text != "snippets" {
				return fmt.Errorf("allowed values: %s", s.Range)
			}
		case "summaries.model":
			if strings.TrimSpace(text) == "" {
				return fmt.Errorf("must not be empty")
			}
		}
		v = text
	case "list":
		var list []string
		switch a := raw.(type) {
		case []string:
			list = append([]string(nil), a...)
		case []any:
			for _, x := range a {
				text, ok := x.(string)
				if !ok {
					return fmt.Errorf("expected string list")
				}
				list = append(list, text)
			}
		default:
			return fmt.Errorf("expected string list")
		}
		if s.Key == "roots" || s.Key == "orphans.extra_signatures" {
			for i := range list {
				list[i] = ExpandHome(list[i])
			}
		}
		if s.Key == "cleanup.regenerable" {
			merged := append([]string(nil), c.Cleanup.Regenerable...)
			for _, p := range list {
				if p == "" || filepath.IsAbs(p) || strings.Contains(p, "..") || strings.Trim(p, "/") == "" {
					return fmt.Errorf("expected relative directory without ..")
				}
				found := false
				for _, existing := range merged {
					if existing == p {
						found = true
					}
				}
				if !found {
					merged = append(merged, p)
				}
			}
			list = merged
		}
		v = list
	}
	if strings.HasPrefix(s.Key, "adapters.") {
		c.Adapters[strings.Split(s.Key, ".")[1]] = AdapterConfig{Enabled: v.(bool)}
	} else {
		s.field(c).Set(reflect.ValueOf(v))
	}
	return nil
}

// Load reads strict TOML then environment; missing files retain defaults.
func Load(path string) (Config, error) { c, _, err := LoadWithSources(path); return c, err }

// LoadWithSources reports default/file/env provenance for every setting.
func LoadWithSources(path string) (Config, map[string]string, error) {
	return load(path, nil)
}

// LoadWithOverrides applies explicit CLI settings before related-field validation.
// Values use the same native types as TOML (durations/sizes are strings).
func LoadWithOverrides(path string, overrides map[string]any) (Config, error) {
	c, _, err := load(path, overrides)
	return c, err
}

func load(path string, overrides map[string]any) (Config, map[string]string, error) {
	c := Default()
	sources := map[string]string{}
	byKey := map[string]Setting{}
	allowedEnv := map[string]bool{}
	for _, s := range runtimeSettings {
		allowedEnv[s.Name] = true
	}
	for _, s := range settings {
		sources[s.Key] = "default"
		byKey[s.Key] = s
		if name := s.EnvName(); name != "" {
			allowedEnv[name] = true
		}
	}
	for _, pair := range os.Environ() {
		name, _, _ := strings.Cut(pair, "=")
		if strings.HasPrefix(name, "DRIGA_") && !allowedEnv[name] {
			return c, sources, fmt.Errorf("unknown environment name %s", name)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.Getenv("DRIGA_CONFIG") != "" && path == Path() {
			return c, sources, fmt.Errorf("load DRIGA_CONFIG: %w", err)
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return c, sources, err
		}
	}
	if err == nil {
		var values map[string]any
		if err := toml.Unmarshal(b, &values); err != nil {
			return c, sources, fmt.Errorf("config %s: %w", path, err)
		}
		var apply func(string, map[string]any) error
		apply = func(prefix string, values map[string]any) error {
			keys := make([]string, 0, len(values))
			for key := range values {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				name := prefix + key
				value := values[key]
				if s, ok := byKey[name]; ok {
					if err := s.assign(&c, value); err != nil {
						return fmt.Errorf("config %s: %s: %w", path, name, err)
					}
					sources[name] = "file"
					continue
				}
				section, ok := value.(map[string]any)
				known := false
				if ok {
					for full := range byKey {
						if strings.HasPrefix(full, name+".") {
							known = true
							break
						}
					}
				}
				if !known {
					return fmt.Errorf("config %s: unknown key or section %s", path, name)
				}
				if err := apply(name+".", section); err != nil {
					return err
				}
			}
			return nil
		}
		if err := apply("", values); err != nil {
			return c, sources, err
		}
	}
	for _, s := range settings {
		name := s.EnvName()
		if name == "" {
			continue
		}
		text := os.Getenv(name)
		if text == "" {
			continue
		}
		var raw any = text
		switch s.Type {
		case "bool":
			if text != "true" && text != "false" {
				return c, sources, fmt.Errorf("%s: expected true or false", name)
			}
			raw = text == "true"
		case "int":
			n, err := strconv.ParseInt(text, 10, 64)
			if err != nil {
				return c, sources, fmt.Errorf("%s: expected integer; allowed range %s", name, s.Range)
			}
			raw = n
		}
		if err := s.assign(&c, raw); err != nil {
			return c, sources, fmt.Errorf("%s: %w", name, err)
		}
		sources[s.Key] = "env"
	}
	for key, value := range overrides {
		s, ok := byKey[key]
		if !ok {
			return c, sources, fmt.Errorf("unknown flag setting %s", key)
		}
		if err := s.assign(&c, value); err != nil {
			return c, sources, fmt.Errorf("flag %s: %w", key, err)
		}
		sources[key] = "flag"
	}
	input := func(key string) string {
		if sources[key] == "env" {
			return byKey[key].EnvName()
		}
		if sources[key] == "flag" {
			return "flag " + key
		}
		return "config " + path + ": " + key
	}
	for _, pattern := range c.Exclude {
		if !doublestar.ValidatePattern(strings.Trim(filepath.ToSlash(pattern), "/")) {
			return c, sources, fmt.Errorf("%s: malformed exclude pattern %q", input("exclude"), pattern)
		}
	}
	if c.Fleet.Serve && strings.TrimSpace(c.Fleet.Listen) == "" {
		return c, sources, fmt.Errorf("%s: fleet.listen is required when fleet.serve is true", input("fleet.listen"))
	}

	host, port, err := net.SplitHostPort(c.Intake.Listen)
	ip := net.ParseIP(host)
	p, e := strconv.Atoi(port)
	if err != nil || ip == nil || !ip.IsLoopback() || e != nil || p < 0 || p > 65535 {
		return c, sources, fmt.Errorf("%s: intake.listen requires a loopback IP and numeric TCP port 0..65535", input("intake.listen"))
	}
	return c, sources, nil
}

// EnvExample generates the tracked template without active assignments. Only
// the fixed setting inventory is used: never resolve runtime OS or home paths.
func EnvExample() string {
	var b strings.Builder
	b.WriteString("# Generated by go generate ./internal/config; do not edit.\n# Driga reads the process environment, never dotenv files.\n# Precedence: explicit flags > environment > TOML file > defaults.\n# Empty environment values are unset. All settings are optional unless stated.\n# Restart collector after changing its configuration; CLI/hooks reload per invocation.\n\n")
	for _, s := range settings {
		fmt.Fprintf(&b, "# %s: %s; default=%v; optional. %s\n# Allowed: %s.\n", s.Key, s.Type, s.Default, s.Purpose, s.Range)
		if name := s.EnvName(); name != "" {
			fmt.Fprintf(&b, "# %s=\n", name)
		} else {
			b.WriteString("# File-only structured list; no environment separator.\n")
		}
		b.WriteByte('\n')
	}
	b.WriteString("# Path overrides and internal/test namespace extensions.\n")
	for _, s := range runtimeSettings {
		fmt.Fprintf(&b, "# %s: %s; default=%s; optional. %s\n# %s=\n\n", s.Name, s.Type, s.Default, s.Purpose, s.Name)
	}
	return b.String()
}
