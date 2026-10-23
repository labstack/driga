package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/labstack/driga/internal/secfile"

	"github.com/kballard/go-shellquote"
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
	if g.JSON {
		return json.NewEncoder(commandOutput(g)).Encode(map[string]any{"schema": 2, "data": map[string]string{"path": config.Path()}})
	}
	_, err := fmt.Fprintln(commandOutput(g), config.Path())
	return err
}

// ConfigShowCmd prints all effective values.
type ConfigShowCmd struct{}

// Run loads overrides and prints defaults for unspecified settings.
func (ConfigShowCmd) Run(g *Globals) error {
	if g.JSON {
		return errors.New("config show prints TOML; --json is not supported")
	}
	cfg, sources, err := config.LoadWithSources(config.Path())
	if err != nil {
		return err
	}
	_, err = io.WriteString(commandOutput(g), configTextSources(cfg, false, nil, sources))
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
	args, err := shellquote.Split(editor)
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
	return configTextSources(c, commented, chosen, nil)
}

func configTextSources(c config.Config, commented bool, chosen map[string]bool, sources map[string]string) string {
	var b strings.Builder
	defaults := config.Default()
	if commented {
		b.WriteString("# Driga configuration. Commented settings use built-in defaults.\n# Precedence: explicit flags > environment > this file > defaults.\n# Restart collector after changes; CLI/hooks reload per invocation.\n# Roots add repo discovery; session working directories are always discovered.\n# Snippets are opt-in, scrubbed, and limited to 200 characters.\n# open_cmd is auto-detected when empty; {cwd} is the session directory.\n\n")
	}
	value := func(s config.Setting, c config.Config) any {
		v := s.Value(c)
		switch s.Type {
		case "duration":
			return durationText(v.(time.Duration))
		case "size":
			return fmt.Sprint(v)
		case "list":
			if v.([]string) == nil {
				return []string{}
			}
		}
		return v
	}
	sectionOf := func(key string) string {
		i := strings.LastIndexByte(key, '.')
		if i < 0 {
			return ""
		}
		return key[:i]
	}
	definitions := config.Settings()
	section := ""
	for _, s := range definitions {
		next := sectionOf(s.Key)
		if next != section {
			section = next
			b.WriteByte('\n')
			active := !commented
			for _, peer := range definitions {
				if sectionOf(peer.Key) == section && (chosen[peer.Key] || !reflect.DeepEqual(value(peer, c), value(peer, defaults))) {
					active = true
				}
			}
			if !active {
				b.WriteString("# ")
			}
			fmt.Fprintf(&b, "[%s]\n", section)
		}
		if sources != nil {
			fmt.Fprintf(&b, "# source: %s\n", sources[s.Key])
		}
		if commented && !chosen[s.Key] && reflect.DeepEqual(value(s, c), value(s, defaults)) {
			b.WriteString("# ")
		}
		key := strings.TrimPrefix(s.Key, section+".")
		data, _ := toml.Marshal(map[string]any{key: value(s, c)})
		b.Write(data)
	}
	return b.String()
}

func writeConfig(path, text string) error {
	dir := filepath.Dir(path)
	if err := secfile.MkdirPrivate(dir); err != nil {
		return err
	}
	if err := secfile.Protect(dir); err != nil {
		return err
	}
	return secfile.WriteFileAtomic(path, []byte(text), nil)
}
