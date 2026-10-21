package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
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
	Target string `arg:"" optional:"" help:"Agent name (claude, codex, copilot, or all); overrides --agent."`
	DryRun bool   `help:"Print the diff without writing or prompting."`
	Agent  string `enum:"claude,codex,copilot,all" default:"all" help:"Agent to configure: claude, codex, copilot, or all."`
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
type HooksStatusCmd struct{ HooksOptions }

// Run prints the selected agents' hook status.
func (c HooksStatusCmd) Run(g *Globals) error {
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
	if agent == "copilot" || agent == "all" {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		status, err := hooks.CopilotStatus(copilotHookPath(), stablePath(exe))
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintln(out, "Copilot:", status); err != nil {
			return err
		}
	}
	if agent == "claude" || agent == "all" {
		events, err := hooks.ClaudeStatus(claude)
		if err != nil {
			return err
		}
		status := "not installed"
		if len(events) > 0 {
			status = strings.Join(events, ", ")
		}
		if _, err := fmt.Fprintln(out, "Claude:", status); err != nil {
			return err
		}
	}
	if agent == "codex" || agent == "all" {
		// A removal preview identifies our exact notify argv without writing.
		d, err := hooks.CodexUninstall(codex, true)
		if err != nil {
			return err
		}
		status := "not installed"
		if d.Changed {
			status = "installed"
		}
		if _, err := fmt.Fprintln(out, "Codex:", status); err != nil {
			return err
		}
	}
	return nil
}

func (c HooksOptions) agent() (string, error) {
	if c.Target != "" {
		c.Agent = c.Target
	}
	switch c.Agent {
	case "":
		return "all", nil
	case "claude", "codex", "copilot", "all":
		return c.Agent, nil
	default:
		return "", fmt.Errorf("unknown hook agent %q (use claude, codex, copilot, or all)", c.Agent)
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
		return fmt.Errorf("--traces requires --otlp")
	}
	agent, err := c.agent()
	if err != nil {
		return err
	}
	if c.OTLP && agent == "copilot" {
		return errors.New("Copilot supports hooks only; OTLP is not supported")
	}
	claude, codex, err := hookPaths()
	if err != nil {
		return err
	}
	var driga string
	if install && !c.OTLP {
		driga, err = os.Executable()
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
	if _, err := fmt.Fprint(out, "Apply? [y/N] "); err != nil {
		return err
	}
	if g.Stdin == nil {
		return nil
	}
	answer, err := bufio.NewReader(g.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "y" && answer != "yes" {
		return nil
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
		return errors.New("Copilot supports hooks only; OTLP is not supported")
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
