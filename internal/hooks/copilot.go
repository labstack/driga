package hooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/labstack/driga/internal/secfile"
	"os"
	"path/filepath"
	"time"
)

var copilotEvents = []string{"sessionStart", "userPromptSubmitted", "postToolUse", "postToolUseFailure", "notification", "agentStop", "errorOccurred", "subagentStart", "subagentStop", "sessionEnd"}

type copilotCommand struct {
	Type       string   `json:"type"`
	Exec       string   `json:"exec"`
	Args       []string `json:"args"`
	TimeoutSec int      `json:"timeoutSec"`
}
type copilotConfig struct {
	Version int                         `json:"version"`
	Hooks   map[string][]copilotCommand `json:"hooks"`
}

// CopilotPath resolves the one file owned by Driga, without inspecting user files.
func CopilotPath(home, copilotHome string) string {
	if copilotHome == "" {
		copilotHome = filepath.Join(home, ".copilot")
	}
	return filepath.Join(copilotHome, "hooks", "driga.json")
}

func copilotSettings(driga string) copilotConfig {
	c := copilotConfig{Version: 1, Hooks: map[string][]copilotCommand{}}
	for _, event := range copilotEvents {
		c.Hooks[event] = []copilotCommand{{Type: "command", Exec: driga, Args: []string{"hook", "copilot", event}, TimeoutSec: 3}}
	}
	return c
}

func readCopilot(path string) (Diff, error) {
	// An owned file must never redirect a write/delete into another config.
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return Diff{}, fmt.Errorf("Copilot hook file must be a regular file: %s", path)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Diff{}, err
	}
	d, err := readDiff(path)
	d.owned = true
	return d, err
}

// CopilotInstall previews or atomically replaces only hooks/driga.json.
func CopilotInstall(path, driga string, dryRun bool, now time.Time) (Diff, error) {
	d, err := readCopilot(path)
	if err != nil {
		return d, err
	}
	if !filepath.IsAbs(driga) {
		return d, fmt.Errorf("driga binary path must be absolute: %q", driga)
	}
	b, err := json.MarshalIndent(copilotSettings(driga), "", "  ")
	if err != nil {
		return d, err
	}
	d.After = string(b) + "\n"
	d.Changed = d.Before != d.After
	return d, writeDiff(d, dryRun, now)
}

// CopilotUninstall removes only the owned file, leaving every other hook intact.
func CopilotUninstall(path string, dryRun bool) (Diff, error) {
	d, err := readCopilot(path)
	if err != nil {
		return d, err
	}
	d.After = ""
	d.Changed = d.existed
	return d, writeDiff(d, dryRun, time.Now())
}

// CopilotStatus checks executable ownership and existence, allowing other valid Driga paths.
func CopilotStatus(path, driga string) (string, error) {
	d, err := readCopilot(path)
	if err != nil {
		return "", err
	}
	if !d.existed {
		return "missing", nil
	}
	var c copilotConfig
	if err := json.Unmarshal([]byte(d.Before), &c); err != nil {
		return "missing", nil
	}
	commandsFound := false
	for _, commands := range c.Hooks {
		for _, command := range commands {
			commandsFound = true
			info, err := os.Stat(command.Exec)
			if err != nil || !info.Mode().IsRegular() || !ours(command.Exec, driga) {
				return "stale", nil
			}
		}
	}
	if !commandsFound {
		return "missing", nil
	}
	return "installed", nil
}

func writeOwnedDiff(d Diff) error {
	if _, err := readCopilot(d.Path); err != nil {
		return err
	}
	if d.After == "" {
		return os.Remove(d.Path)
	}
	if err := secfile.MkdirPrivate(filepath.Dir(d.Path)); err != nil {
		return err
	}
	f, err := secfile.CreateTemp(filepath.Dir(d.Path), ".driga-hooks-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.WriteString(d.After)
	err = errors.Join(writeErr, f.Sync(), f.Close())
	if err != nil {
		return err
	}
	if err := d.Check(); err != nil {
		return err
	}
	return renameSettings(f.Name(), d.Path)
}
