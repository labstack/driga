package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/pelletier/go-toml/v2"
)

// ConfigCmd inspects or edits the user configuration.
type ConfigCmd struct {
	Path ConfigPathCmd `cmd:"" help:"Print the config path."`
	Show ConfigShowCmd `cmd:"" help:"Print effective values including defaults."`
	Edit ConfigEditCmd `cmd:"" help:"Edit the config with $EDITOR (default vi)."`
}

// ConfigPathCmd prints the resolved configuration path.
type ConfigPathCmd struct{}

// Run prints the path without creating a file.
func (ConfigPathCmd) Run(g *Globals) error {
	_, err := fmt.Fprintln(commandOutput(g), config.Path())
	return err
}

// ConfigShowCmd prints all effective values.
type ConfigShowCmd struct{}

// Run loads overrides and prints defaults for unspecified settings.
func (ConfigShowCmd) Run(g *Globals) error {
	cfg, err := config.Load(config.Path())
	if err != nil {
		return err
	}
	_, err = io.WriteString(commandOutput(g), configText(cfg, false, nil))
	return err
}

// ConfigEditCmd opens the configuration in the user's editor.
type ConfigEditCmd struct{}

// Run creates commented defaults only if missing, then waits for the editor.
func (ConfigEditCmd) Run(g *Globals) error {
	path := config.Path()
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := writeConfig(path, configText(config.Default(), true, nil)); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	editor := os.Getenv("EDITOR")
	if strings.TrimSpace(editor) == "" {
		editor = "vi"
	}
	// Parse quoting without running a shell: filenames and editor flags remain argv.
	args, err := splitCommandWords(editor)
	if err != nil {
		return fmt.Errorf("EDITOR: %w", err)
	}
	if len(args) == 0 || args[0] == "" {
		return errors.New("EDITOR must name an executable")
	}
	cmd := exec.Command(args[0], append(args[1:], path)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = g.Stdin, commandOutput(g), g.Stderr
	return cmd.Run()
}

func splitCommandWords(s string) ([]string, error) {
	var args []string
	var word strings.Builder
	var quote rune
	escaped, started := false, false
	flush := func() {
		if started {
			args = append(args, word.String())
			word.Reset()
			started = false
		}
	}
	for _, r := range s {
		switch {
		case escaped:
			word.WriteRune(r)
			escaped = false
			started = true
		case r == '\\' && quote != '\'':
			escaped = true
			started = true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
			started = true
		case r == ' ' || r == '\t' || r == '\n':
			flush()
		default:
			word.WriteRune(r)
			started = true
		}
	}
	if escaped || quote != 0 {
		return nil, errors.New("command contains unfinished quoting")
	}
	flush()
	return args, nil
}

func durationText(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

func configText(c config.Config, commented bool, chosen map[string]bool) string {
	var b strings.Builder
	defaults := config.Default()
	if commented {
		b.WriteString("# Driga configuration. Commented settings use built-in defaults.\n# Roots add repo discovery; session working directories are always discovered.\n# Snippets are opt-in, scrubbed, and limited to 200 characters.\n# open_cmd is auto-detected when empty; {cwd} is the session directory.\n\n")
	}
	list := func(v []string) []string {
		if v == nil {
			return []string{}
		}
		return v
	}
	line := func(key string, value, def any) {
		if commented && !chosen[key] && reflect.DeepEqual(value, def) {
			b.WriteString("# ")
		}
		// All supplied values are strings, booleans, integers, or string slices.
		data, _ := toml.Marshal(map[string]any{key: value})
		b.Write(data)
	}
	line("roots", list(c.Roots), list(defaults.Roots))
	line("root_depth", c.RootDepth, defaults.RootDepth)
	line("exclude", list(c.Exclude), list(defaults.Exclude))
	line("stuck_after", durationText(c.StuckAfter), durationText(defaults.StuckAfter))
	line("orphan_min_age", durationText(c.OrphanMinAge), durationText(defaults.OrphanMinAge))
	line("capture_text", c.CaptureText, defaults.CaptureText)
	line("no_service_hint", c.NoServiceHint, defaults.NoServiceHint)
	line("open_cmd", c.OpenCmd, defaults.OpenCmd)
	names := make([]string, 0, len(c.Adapters))
	for name := range c.Adapters {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		b.WriteByte('\n')
		_, exists := defaults.Adapters[name]
		active := !commented || !exists || c.Adapters[name] != defaults.Adapters[name]
		// Marshal the header so extension adapter names remain valid TOML keys.
		data, _ := toml.Marshal(map[string]any{"adapters": map[string]any{name: map[string]any{"enabled": c.Adapters[name].Enabled}}})
		for _, text := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if text == "[adapters]" || text == "" {
				continue
			}
			if !active {
				b.WriteString("# ")
			}
			b.WriteString(text + "\n")
		}
	}
	b.WriteByte('\n')
	if commented && reflect.DeepEqual(list(c.ExtraSignatures), list(defaults.ExtraSignatures)) {
		b.WriteString("# ")
	}
	b.WriteString("[orphans]\n")
	line("extra_signatures", list(c.ExtraSignatures), list(defaults.ExtraSignatures))
	b.WriteByte('\n')
	if commented && c.Summaries == defaults.Summaries {
		b.WriteString("# ")
	}
	b.WriteString("[summaries]\n")
	line("enabled", c.Summaries.Enabled, defaults.Summaries.Enabled)
	line("model", c.Summaries.Model, defaults.Summaries.Model)
	b.WriteByte('\n')
	if commented && reflect.DeepEqual(c.Cleanup, defaults.Cleanup) {
		b.WriteString("# ")
	}
	b.WriteString("[cleanup]\n")
	line("worktree_idle", durationText(c.Cleanup.WorktreeIdle), durationText(defaults.Cleanup.WorktreeIdle))
	line("low_disk_percent", c.Cleanup.LowDiskPercent, defaults.Cleanup.LowDiskPercent)
	line("low_disk_bytes", fmt.Sprint(c.Cleanup.LowDiskBytes), fmt.Sprint(defaults.Cleanup.LowDiskBytes))
	line("regenerable", c.Cleanup.Regenerable, defaults.Cleanup.Regenerable)
	return b.String()
}

func writeConfig(path, text string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".config-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := io.WriteString(f, text)
	if err := errors.Join(writeErr, f.Close()); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
