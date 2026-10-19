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
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

type CleanupCmd struct {
	ID             string `arg:"" optional:"" help:"Candidate ID to clean after confirmation."`
	Kind           string `help:"Filter by worktree, scratch, or history (required with --all)."`
	Sizes          bool   `help:"Walk candidate sizes on demand (30s cap per walk)."`
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
	env = cleanup.Env{Store: st, Plat: cleanupPlatform(), Home: config.ExpandHome("~"), DataDir: config.DataDir(), Cfg: cfg.Cleanup}
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

func cleanupRead(ctx context.Context, g *Globals, kind string, sizes bool) (view.Envelope, error) {
	env, envelope, closeFn, err := openCleanup(ctx, g)
	if err != nil {
		return envelope, err
	}
	defer closeFn()
	env.Sizes = sizes
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
		return fmt.Errorf("unknown cleanup kind %q", c.Kind)
	}
	if c.ID != "" && c.All {
		return fmt.Errorf("provide an id or --all")
	}
	if c.All && c.Kind == "" {
		return fmt.Errorf("--all requires --kind")
	}
	if c.IncludeIgnored && (c.ID == "" || c.All) {
		return fmt.Errorf("--include-ignored requires one explicit candidate ID")
	}
	if c.ID == "" && !c.All && (c.Yes || c.DryRun || c.IncludeIgnored) {
		return fmt.Errorf("action flags require an id or --all")
	}
	ctx := context.Background()
	if c.ID == "" && !c.All {
		envelope, err := cleanupRead(ctx, g, c.Kind, c.Sizes)
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
		var report strings.Builder
		local := *g
		local.JSON = false
		local.Stdout = &report
		if err := c.runCandidates(ctx, &local, env, cs); err != nil {
			return err
		}
		envelope.Data = struct {
			Report string `json:"report"`
		}{report.String()}
		return json.NewEncoder(commandOutput(g)).Encode(envelope)
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
			if _, err := fmt.Fprintf(out, "%s %s %s %s %s\n", c.Kind, c.ID, c.Status, c.Path, size); err != nil {
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
