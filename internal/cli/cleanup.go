package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/scrub"
	"github.com/labstack/driga/internal/signatures"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

type CleanupCmd struct {
	ID             string `arg:"" optional:"" help:"Candidate ID to clean after confirmation."`
	Kind           string `help:"Filter by worktree, scratch, or history (required with --all)."`
	Sizes          bool   `help:"Walk candidate sizes on demand (30s cap per walk)."`
	Why            bool   `help:"Also list worktrees that do not qualify, with the reason."`
	All            bool   `help:"Clean ready candidates of --kind; skip candidates that need review or change."`
	DryRun         bool   `help:"Print exact commands after verification without removing anything."`
	Yes            bool   `help:"Confirm the displayed action without prompting."`
	IncludeIgnored bool   `help:"Allow listed non-regenerable ignored files for one explicit ID."`
}

var cleanupPlatform = platform.New

// openCleanup reads stored local attribution without invoking the collector or
// adapters. A missing DB supplies empty in-memory rows, never a persistent write.
func openCleanup(ctx context.Context, g *Globals) (cleanup.Env, view.Envelope, func(), error) {
	var env cleanup.Env
	var envelope view.Envelope
	cfg, err := config.Load(config.Path())
	if err != nil {
		return env, envelope, nil, err
	}
	sigs, err := signatures.Load(cfg.ExtraSignatures)
	if err != nil {
		return env, envelope, nil, err
	}
	path := filepath.Join(config.DataDir(), "driga.db")
	mode := "daemon"
	st, err := store.Open(path, store.ReadOnly)
	if err != nil {
		if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
			return env, envelope, nil, err
		}
		st, err = store.Open("", store.Memory)
		mode = "snapshot"
	}
	if err != nil {
		return env, envelope, nil, err
	}
	closeFn := func() { _ = st.Close() }
	env = cleanup.Env{Store: st, Plat: cleanupPlatform(), Sigs: sigs, Home: config.ExpandHome("~"), DataDir: config.DataDir(), Cfg: cfg.Cleanup}
	if g.CleanupPlatform != nil {
		env.Plat = g.CleanupPlatform
	}
	err = st.Tx(ctx, func(tx *store.Tx) error {
		scope, err := resolveMachine(g, tx)
		if err != nil {
			return err
		}
		if scope.Machines != nil {
			for _, id := range scope.Machines {
				if id != st.MachineID() {
					return fmt.Errorf("cleanup is local-only; run driga cleanup on that machine")
				}
			}
		}
		envelope, err = view.NewEnvelope(tx, mode, time.Now(), collector.Cadence(0), nil, store.Scope{Machines: []string{st.MachineID()}})
		return err
	})
	if err != nil {
		closeFn()
		return env, envelope, nil, err
	}
	return env, envelope, closeFn, nil
}

func cleanupRead(ctx context.Context, g *Globals, kind string, sizes, why bool) (view.Envelope, error) {
	env, envelope, closeFn, err := openCleanup(ctx, g)
	if err != nil {
		return envelope, err
	}
	defer closeFn()
	env.Sizes, env.Why = sizes, why
	cs, err := cleanup.List(ctx, env)
	if err != nil {
		return envelope, err
	}
	selected := []cleanup.Candidate{}
	for _, c := range cs {
		if kind == "" || string(c.Kind) == kind {
			selected = append(selected, c)
		}
	}
	envelope.Data = selected
	return envelope, nil
}
func (c CleanupCmd) Run(g *Globals) error {
	if c.Kind != "" && c.Kind != "worktree" && c.Kind != "scratch" && c.Kind != "history" {
		return invalidUsage("unknown cleanup kind %q", c.Kind)
	}
	if c.ID != "" && c.All {
		return invalidUsage("provide an id or --all")
	}
	if c.All && c.Kind == "" {
		return invalidUsage("--all requires --kind")
	}
	if c.IncludeIgnored && (c.ID == "" || c.All) {
		return invalidUsage("--include-ignored requires one explicit candidate ID")
	}
	if c.Why && (c.ID != "" || c.All) {
		return invalidUsage("--why only applies to listing")
	}
	if c.ID == "" && !c.All && (c.Yes || c.DryRun || c.IncludeIgnored) {
		return invalidUsage("action flags require an id or --all")
	}
	ctx, stop := notifyContext(context.Background())
	defer stop()
	if c.ID == "" && !c.All {
		envelope, err := cleanupRead(ctx, g, c.Kind, c.Sizes, c.Why)
		if err != nil {
			return err
		}
		if g.JSON {
			return json.NewEncoder(commandOutput(g)).Encode(envelope)
		}
		return printCleanup(commandOutput(g), envelope.Data.([]cleanup.Candidate), c.Sizes)
	}
	env, envelope, closeFn, err := openCleanup(ctx, g)
	if err != nil {
		return err
	}
	defer closeFn()
	env.Sizes = c.Sizes
	cs, err := cleanup.List(ctx, env)
	if err != nil {
		return err
	}
	if g.JSON {
		outcomes, actionErr := c.jsonCandidates(ctx, env, cs)
		envelope.Data = struct {
			Outcomes []cleanupOutcome `json:"outcomes"`
		}{outcomes}
		if actionErr != nil {
			envelope.Error = &view.ErrorRecord{Code: "cleanup_failed", Message: scrub.Scrub(actionErr.Error())}
		}
		if err := json.NewEncoder(commandOutput(g)).Encode(envelope); err != nil {
			return reportedError{err}
		}
		if actionErr != nil {
			return reportedError{actionErr}
		}
		return nil
	}
	return c.runCandidates(ctx, g, env, cs)
}
func (c CleanupCmd) runCandidates(ctx context.Context, g *Globals, env cleanup.Env, cs []cleanup.Candidate) error {
	out := commandOutput(g)
	input := g.Stdin
	if input == nil {
		input = strings.NewReader("")
	}
	reader := bufio.NewReader(input)
	found := false
	for _, candidate := range cs {
		if c.ID != "" && c.ID != candidate.ID || c.Kind != "" && c.Kind != string(candidate.Kind) {
			continue
		}
		found = true
		skip := func(err error) error {
			if c.All {
				_, writeErr := fmt.Fprintf(out, "%s: skipped: %s\n", candidate.ID, err)
				return writeErr
			}
			return err
		}
		if candidate.Status != "ready" && c.All {
			if err := skip(fmt.Errorf("%s", candidate.Status)); err != nil {
				return err
			}
			continue
		}
		if !c.DryRun && !c.Yes {
			file, ok := input.(*os.File)
			if !ok || !view.IsTerminal(file) {
				return fmt.Errorf("cleanup requires --yes without a terminal")
			}
		}
		// Show the exact evidence, including every ignored path, before consent.
		if err := printCleanup(out, []cleanup.Candidate{candidate}, c.Sizes); err != nil {
			return err
		}
		if !c.DryRun && !c.Yes {
			yes, err := confirmAction(reader, out, "Clean "+candidate.ID+"? [y/N]")
			if err != nil {
				return err
			}
			if !yes {
				continue
			}
		}
		result, err := cleanup.Apply(ctx, env, candidate, cleanup.ApplyOpts{DryRun: c.DryRun, IncludeIgnored: c.IncludeIgnored})
		if err != nil {
			if err := skip(err); err != nil {
				return err
			}
			continue
		}
		for _, cmd := range result.Commands {
			words := make([]string, len(cmd))
			for i, word := range cmd {
				words[i] = shellWord(word)
			}
			if _, err := fmt.Fprintln(out, strings.Join(words, " ")); err != nil {
				return err
			}
		}
		for _, w := range result.Warnings {
			if _, err := fmt.Fprintf(out, "%s: warning: %s\n", candidate.ID, w); err != nil {
				return err
			}
		}
	}
	if !found && !c.All {
		return fmt.Errorf("unknown or no longer eligible cleanup id %q", c.ID)
	}
	return nil
}
func shellWord(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n\r'\"\\$`;|&<>()*?![]{}~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
func printCleanup(out io.Writer, cs []cleanup.Candidate, sizes bool) error {
	for _, kind := range []cleanup.Kind{cleanup.Worktree, cleanup.Scratch, cleanup.History} {
		for _, c := range cs {
			if c.Kind != kind {
				continue
			}
			size := ""
			if sizes {
				size = strconv.FormatInt(c.Size, 10) + " bytes"
				if c.SizeCapped {
					size = "≥" + size
				}
			}
			if _, err := fmt.Fprintf(out, "%s %s %s %s %s\n", c.Kind, c.ID, view.FriendlyEnum(c.Status), c.Path, size); err != nil {
				return err
			}
			for _, e := range c.Evidence {
				if _, err := fmt.Fprintln(out, "  "+e); err != nil {
					return err
				}
			}
			for _, ignored := range c.Ignored {
				class := "non-regenerable"
				if ignored.Regenerable {
					class = "regenerable"
				}
				if sizes {
					class += fmt.Sprintf(", %d bytes", ignored.Size)
				}
				if _, err := fmt.Fprintf(out, "  ignored %q (%s)\n", ignored.Path, class); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

type cleanupOutcome struct {
	ID     string            `json:"id"`
	Kind   cleanup.Kind      `json:"kind"`
	Status string            `json:"status"`
	Result *cleanup.Result   `json:"result,omitempty"`
	Error  *view.ErrorRecord `json:"error,omitempty"`
	Reason string            `json:"reason,omitempty"`
}

func (c CleanupCmd) jsonCandidates(ctx context.Context, env cleanup.Env, cs []cleanup.Candidate) ([]cleanupOutcome, error) {
	outcomes := []cleanupOutcome{}
	var failures []error
	for _, candidate := range cs {
		if c.ID != "" && c.ID != candidate.ID || c.Kind != "" && c.Kind != string(candidate.Kind) {
			continue
		}
		outcome := cleanupOutcome{ID: candidate.ID, Kind: candidate.Kind, Status: "unchanged"}
		switch {
		case ctx.Err() != nil:
			outcome.Status = "cancelled"
			outcome.Error = &view.ErrorRecord{Code: "cancelled", Message: ctx.Err().Error()}
			failures = append(failures, ctx.Err())
		case c.All && candidate.Status != "ready":
			outcome.Reason = candidate.Status
		case !c.DryRun && !c.Yes:
			err := fmt.Errorf("cleanup --json requires --yes to apply; use --dry-run to preview")
			outcome.Status = "cancelled"
			outcome.Error = &view.ErrorRecord{Code: "confirmation_required", Message: scrub.Scrub(err.Error())}
			failures = append(failures, err)
		default:
			result, err := cleanup.Apply(ctx, env, candidate, cleanup.ApplyOpts{DryRun: c.DryRun, IncludeIgnored: c.IncludeIgnored})
			outcome.Result = &result
			if err != nil {
				outcome.Status = "failed"
				if errors.Is(err, context.Canceled) {
					outcome.Status = "cancelled"
				}
				outcome.Error = &view.ErrorRecord{Code: "cleanup_failed", Message: scrub.Scrub(err.Error())}
				failures = append(failures, err)
			} else if !c.DryRun {
				outcome.Status = "completed"
			}
		}
		outcomes = append(outcomes, outcome)
	}
	if len(outcomes) == 0 && !c.All {
		failures = append(failures, fmt.Errorf("unknown or no longer eligible cleanup id %q", c.ID))
	}
	return outcomes, errors.Join(failures...)
}
