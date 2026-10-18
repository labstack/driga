package hooks

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// ErrNotifyInUse means another program already owns Codex's notify setting.
var ErrNotifyInUse = errors.New("Codex notify is already in use")

// CodexInstall sets notify only when no command is configured.
func CodexInstall(configPath, driga string, dryRun bool, now time.Time) (Diff, error) {
	d, settings, err := readCodex(configPath)
	if err != nil {
		return d, err
	}
	if !filepath.IsAbs(driga) {
		return d, fmt.Errorf("driga binary path must be absolute: %q", driga)
	}
	if value, exists := settings["notify"]; exists {
		if codexNotifyOurs(value, driga) {
			return d, nil
		}
		return d, fmt.Errorf("%w: existing notify = %#v", ErrNotifyInUse, value)
	}
	settings["notify"] = []string{driga, "hook", "codex", "notify"}
	return finishCodex(d, settings, dryRun, now)
}

// CodexUninstall removes notify only when it invokes Driga's Codex hook.
func CodexUninstall(configPath string, dryRun bool) (Diff, error) {
	d, settings, err := readCodex(configPath)
	if err != nil {
		return d, err
	}
	if !codexNotifyOurs(settings["notify"], currentBinary()) {
		return d, nil
	}
	delete(settings, "notify")
	return finishCodex(d, settings, dryRun, time.Now())
}

func codexNotifyOurs(value any, driga string) bool {
	args, ok := value.([]any)
	if !ok || len(args) != 4 {
		return false
	}
	program, ok := args[0].(string)
	return ok && ours(program, driga) && args[1] == "hook" && args[2] == "codex" && args[3] == "notify"
}

func readCodex(path string) (Diff, map[string]any, error) {
	d, err := readDiff(path)
	if err != nil {
		return d, nil, err
	}
	settings := make(map[string]any)
	if err := toml.Unmarshal([]byte(d.Before), &settings); err != nil {
		return d, nil, fmt.Errorf("read Codex config %s: %w", path, err)
	}
	return d, settings, nil
}

func finishCodex(d Diff, settings map[string]any, dryRun bool, now time.Time) (Diff, error) {
	b, err := toml.Marshal(settings)
	if err != nil {
		return d, err
	}
	d.After, d.Changed = string(b), true
	return d, writeDiff(d, dryRun, now)
}
