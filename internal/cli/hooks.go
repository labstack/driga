package cli

import (
	"context"
	"errors"
	"fmt"
	"github.com/labstack/driga/internal/self"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/hooks"
)

// HooksCmd manages optional agent hook installations.
type HooksCmd struct {
	Install   HooksInstallCmd   `cmd:"" help:"Preview and install agent hooks."`
	Uninstall HooksUninstallCmd `cmd:"" help:"Preview and remove Driga's agent hooks."`
	Status    HooksStatusCmd    `cmd:"" help:"Show installed agent hooks."`
}

// HooksOptions selects agents and controls preview-only operation.
type HooksOptions struct {
	Target string `arg:"" name:"adapter" optional:"" default:"all" enum:"claude,codex,copilot,all" help:"Adapter: claude, codex, copilot, or all."`
	DryRun bool   `help:"Print the diff without writing or prompting."`
	Yes    bool   `short:"y" help:"Apply without asking for confirmation."`
	OTLP   bool   `help:"Configure optional metadata-only loopback OTLP instead of hooks."`
	Traces bool   `help:"Include Claude beta spans (requires --otlp)."`
}

// HooksInstallCmd installs hooks after confirmation.
type HooksInstallCmd struct{ HooksOptions }

// Run previews and optionally applies installation changes.
func (c HooksInstallCmd) Run(g *Globals) error {
	return c.HooksOptions.change(g, true)
}

// HooksUninstallCmd removes Driga's hooks after confirmation.
type HooksUninstallCmd struct{ HooksOptions }

// Run previews and optionally applies removal changes.
func (c HooksUninstallCmd) Run(g *Globals) error { return c.HooksOptions.change(g, false) }

// HooksStatusCmd reports installed hooks without writing settings.
type HooksStatusCmd struct {
	Target string `arg:"" name:"adapter" optional:"" default:"all" enum:"claude,codex,copilot,all" help:"Adapter: claude, codex, copilot, or all."`
	OTLP   bool   `help:"Show optional loopback OTLP status."`
}

// Run prints the selected agents' hook status.
func (s HooksStatusCmd) Run(g *Globals) error {
	c := HooksOptions{Target: s.Target, OTLP: s.OTLP}
	if g.JSON {
		return errors.New("hooks status: --json is not supported")
	}
	if c.OTLP {
		return c.otlpStatus(g)
	}
	agent, err := c.agent()
	if err != nil {
		return err
	}
	claude, codex, err := hookPaths()
	if err != nil {
		return err
	}
	out := hooksOutput(g)
	exe, err := self.Executable()
	if err != nil {
		return err
	}
	for _, entry := range []struct{ agent, label, path string }{
		{"copilot", "Copilot", copilotHookPath()}, {"claude", "Claude", claude}, {"codex", "Codex", codex},
	} {
		if agent != "all" && agent != entry.agent {
			continue
		}
		_, detail, err := hookStatus(entry.agent, entry.path, stablePath(exe))
		if err != nil {
			return err
		}
		if strings.Contains(detail, "stale") {
			detail += "; fix: driga hooks install " + entry.agent
		}
		if _, err := fmt.Fprintln(out, entry.label+":", detail); err != nil {
			return err
		}
	}
	return nil
}

// hookStatus is shared by status and doctor so binary drift has one diagnosis.
func hookStatus(agent, path, driga string) (status, detail string, err error) {
	status = "warn"
	switch agent {
	case "claude":
		var events []string
		events, err = hooks.ClaudeStatus(path)
		detail = "optional hooks incomplete or absent"
		if len(events) > 0 {
			detail = strings.Join(events, ", ")
		}
		if err == nil {
			var stale bool
			stale, err = hooks.ClaudeStale(path, driga)
			if stale {
				detail = "stale binary; " + detail
			} else if len(events) >= 5 {
				status = "ok"
			}
		}
	case "codex":
		detail, err = hooks.CodexStatus(path, driga)
		if detail != "not installed" && !strings.HasPrefix(detail, "stale") {
			status = "ok"
		}
	case "copilot":
		detail, err = hooks.CopilotStatus(path, driga)
		if detail == "installed" {
			status = "ok"
		}
	}
	if err != nil {
		status = "fail"
	}
	return
}

func (c HooksOptions) agent() (string, error) {
	switch c.Target {
	case "":
		return "all", nil
	case "claude", "codex", "copilot", "all":
		return c.Target, nil
	default:
		return "", invalidUsage("unknown hook adapter %q (use claude, codex, copilot, or all)", c.Target)
	}
}

func hookPaths() (claude, codex string, err error) {
	home := os.Getenv("HOME")
	if runtime.GOOS == "windows" {
		home = config.ExpandHome("~")
	}
	if home == "" {
		home, err = os.UserHomeDir()
		if err != nil {
			return "", "", err
		}
	}
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	claudeHome := filepath.Join(home, ".claude")
	if runtime.GOOS == "windows" && os.Getenv("CLAUDE_CONFIG_DIR") != "" {
		claudeHome = os.Getenv("CLAUDE_CONFIG_DIR")
	}
	return filepath.Join(claudeHome, "settings.json"), filepath.Join(codexHome, "config.toml"), nil
}

func copilotHookPath() string {
	return hooks.CopilotPath(config.ExpandHome("~"), os.Getenv("COPILOT_HOME"))
}

func hooksOutput(g *Globals) io.Writer {
	if g.Stdout == nil {
		return io.Discard
	}
	return g.Stdout
}

func (c HooksOptions) change(g *Globals, install bool) error {
	if c.Traces && !c.OTLP {
		return invalidUsage("--traces requires --otlp")
	}
	agent, err := c.agent()
	if err != nil {
		return err
	}
	if c.OTLP && agent == "copilot" {
		return invalidUsage("Copilot supports hooks only; OTLP is not supported")
	}
	claude, codex, err := hookPaths()
	if err != nil {
		return err
	}
	var driga string
	if install && !c.OTLP {
		driga, err = self.Executable()
		if err != nil {
			return err
		}
		driga = stablePath(driga)
	}
	endpoint := ""
	if c.OTLP {
		cfg, err := config.Load(config.Path())
		if err != nil {
			return err
		}
		endpoint = "http://" + cfg.Intake.Listen
	}
	now := time.Now()
	var operations []func(bool) (hooks.Diff, error)
	if install && !c.OTLP && (agent == "copilot" || agent == "all") {
		if warning := copilotVersionWarning(context.Background()); warning != "" {
			if _, err := fmt.Fprintln(hooksOutput(g), "Warning:", warning); err != nil {
				return err
			}
		}
	}
	if !c.OTLP && (agent == "copilot" || agent == "all") {
		operations = append(operations, func(dryRun bool) (hooks.Diff, error) {
			if install {
				return hooks.CopilotInstall(copilotHookPath(), driga, dryRun, now)
			}
			return hooks.CopilotUninstall(copilotHookPath(), dryRun)
		})
	}
	if agent == "claude" || agent == "all" {
		operations = append(operations, func(dryRun bool) (hooks.Diff, error) {
			if c.OTLP {
				if install {
					return hooks.ClaudeInstallOTLP(claude, c.Traces, dryRun, now, endpoint)
				}
				return hooks.ClaudeUninstallOTLP(claude, dryRun, endpoint)
			}
			if install {
				return hooks.ClaudeInstall(claude, driga, dryRun, now)
			}
			return hooks.ClaudeUninstall(claude, dryRun)
		})
	}
	if agent == "codex" || agent == "all" {
		operations = append(operations, func(dryRun bool) (hooks.Diff, error) {
			if c.OTLP {
				if install {
					return hooks.CodexInstallOTLP(codex, c.Traces, dryRun, now, endpoint)
				}
				return hooks.CodexUninstallOTLP(codex, dryRun, endpoint)
			}
			if install {
				return hooks.CodexInstall(codex, driga, dryRun, now)
			}
			return hooks.CodexUninstall(codex, dryRun)
		})
	}
	var changes []hooks.Diff
	out := hooksOutput(g)
	for _, operation := range operations {
		d, err := operation(true)
		if err != nil {
			return err
		}
		if d.Changed {
			changes = append(changes, d)
			if _, err := io.WriteString(out, d.Unified()); err != nil {
				return err
			}
		}
	}
	if len(changes) == 0 || c.DryRun {
		return nil
	}
	if err := requireConfirmation(g, c.Yes, "Apply?"); err != nil {
		return err
	}
	// Check all files before writing any preview, then guard each atomic write again.
	for _, change := range changes {
		if err := change.Check(); err != nil {
			return err
		}
	}
	for _, change := range changes {
		if err := change.Apply(now); err != nil {
			return err
		}
	}
	if !install && !c.OTLP && (agent == "copilot" || agent == "all") {
		_, err := fmt.Fprintln(out, "Running Copilot sessions keep calling the removed hook until restarted; Copilot fails open.")
		return err
	}
	return nil
}

func (c HooksOptions) otlpStatus(g *Globals) error {
	agent, err := c.agent()
	if err != nil {
		return err
	}
	if agent == "copilot" {
		return invalidUsage("Copilot supports hooks only; OTLP is not supported")
	}
	claude, codex, err := hookPaths()
	if err != nil {
		return err
	}
	cfg, err := config.Load(config.Path())
	if err != nil {
		return err
	}
	endpoint := "http://" + cfg.Intake.Listen
	for _, a := range []string{"claude", "codex"} {
		if agent != "all" && agent != a {
			continue
		}
		var d hooks.Diff
		if a == "claude" {
			d, err = hooks.ClaudeUninstallOTLP(claude, true, endpoint)
		} else {
			d, err = hooks.CodexUninstallOTLP(codex, true, endpoint)
		}
		if err != nil {
			return err
		}
		status := "not installed"
		if d.Changed {
			status = "installed"
		}
		if _, err := fmt.Fprintf(hooksOutput(g), "%s OTLP: %s\n", a, status); err != nil {
			return err
		}
	}
	return nil
}

// stablePath prefers the PATH entry (for example a package manager's bin
// symlink) when it is this same executable. Resolving it instead would bake an
// upgrade-specific path (Homebrew's Cellar/<version>) into agent settings and
// the service definition, and the next upgrade would leave them pointing at a
// deleted file.
func stablePath(exe string) string {
	path, err := exec.LookPath("driga")
	if err != nil {
		return exe
	}
	current, currentErr := os.Stat(exe)
	candidate, candidateErr := os.Stat(path)
	if currentErr != nil || candidateErr != nil || !os.SameFile(current, candidate) {
		return exe
	}
	if absolute, err := filepath.Abs(path); err == nil {
		return absolute
	}
	return exe
}
