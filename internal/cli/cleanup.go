package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/labstack/driga/internal/actions"

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
	List   CleanupListCmd   `cmd:"" default:"withargs" help:"List local disk cleanup candidates."`
	Remove CleanupRemoveCmd `cmd:"" help:"Verify and remove selected local disk candidates."`
}
type CleanupListCmd struct {
	JSONOptions
	Kind  string `help:"Filter by worktree, scratch, or history."`
	Sizes bool   `help:"Walk candidate sizes on demand."`
	Why   bool   `help:"Include worktrees that do not qualify, with reasons."`
}

func (c CleanupListCmd) Run(g *Globals) error {
	if c.Kind != "" && c.Kind != "worktree" && c.Kind != "scratch" && c.Kind != "history" {
		return invalidUsage("unknown cleanup kind %q", c.Kind)
	}
	envelope, err := cleanupRead(context.Background(), g, c.Kind, c.Sizes, c.Why)
	if err != nil {
		return err
	}
	if g.JSON {
		return json.NewEncoder(commandOutput(g)).Encode(envelope)
	}
	return actions.PrintCleanup(commandOutput(g), envelope.Data.([]cleanup.Candidate), c.Sizes)
}

type CleanupRemoveCmd struct {
	JSONOptions
	ID             string `arg:"" name:"candidate-id" optional:"" help:"Candidate ID to clean after confirmation."`
	Kind           string `help:"Filter by worktree or scratch (required with --all); history is report-only."`
	Sizes          bool   `help:"Walk candidate sizes on demand (30s cap per walk)."`
	All            bool   `help:"Remove ready candidates of --kind; skip candidates needing review and report failures."`
	DryRun         bool   `help:"Print exact commands after verification without removing anything."`
	Yes            bool   `short:"y" help:"Confirm the displayed action without prompting."`
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
func (c CleanupRemoveCmd) Run(g *Globals) error {
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
	ctx, stop := notifyContext(context.Background())
	defer stop()
	if err := actionSelection(c.ID, c.All); err != nil {
		return err
	}
	if c.Kind == "history" {
		return invalidUsage("history is report-only; use --kind worktree or scratch")
	}
	if !c.DryRun && !c.Yes {
		if !terminalInput(g) || g.JSON {
			return invalidUsage("not applied; pass --yes to apply without asking")
		}
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
		counts := map[string]int{"completed": 0, "skipped": 0, "failed": 0}
		for _, outcome := range outcomes {
			switch outcome.Status {
			case "completed":
				counts["completed"]++
			case "failed", "cancelled":
				counts["failed"]++
			default:
				counts["skipped"]++
			}
		}
		envelope.Data = struct {
			Outcomes  []actions.CleanupOutcome `json:"outcomes"`
			Completed int                      `json:"completed"`
			Skipped   int                      `json:"skipped"`
			Failed    int                      `json:"failed"`
		}{outcomes, counts["completed"], counts["skipped"], counts["failed"]}
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
func (c CleanupRemoveCmd) options() actions.CleanupOptions {
	return actions.CleanupOptions{ID: c.ID, Kind: c.Kind, Sizes: c.Sizes, All: c.All, DryRun: c.DryRun, Yes: c.Yes, IncludeIgnored: c.IncludeIgnored}
}
func (c CleanupRemoveCmd) runCandidates(ctx context.Context, g *Globals, env cleanup.Env, cs []cleanup.Candidate) error {
	return c.options().Remove(ctx, actionEnvironment(g, c.Yes), env, cs)
}
func (c CleanupRemoveCmd) jsonCandidates(ctx context.Context, env cleanup.Env, cs []cleanup.Candidate) ([]actions.CleanupOutcome, error) {
	return c.options().JSONCandidates(ctx, env, cs)
}
