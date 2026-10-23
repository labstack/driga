package actions

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/scrub"
	"github.com/labstack/driga/internal/view"
)

type CleanupOptions struct {
	ID, Kind                                string
	Sizes, All, DryRun, Yes, IncludeIgnored bool
}

func (c CleanupOptions) Remove(ctx context.Context, g Environment, env cleanup.Env, cs []cleanup.Candidate) (retErr error) {
	out := g.Stdout
	found := false
	completed, skipped, failed := 0, 0, 0
	var failures []error
	defer func() {
		if c.All {
			_, err := fmt.Fprintf(out, "completed %d; skipped %d; failed %d\n", completed, skipped, failed)
			retErr = errors.Join(retErr, err)
		}
	}()
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
			skipped++
			if err := skip(fmt.Errorf("%s", candidate.Status)); err != nil {
				return err
			}
			continue
		}
		// Show the exact evidence, including every ignored path, before consent.
		if err := PrintCleanup(out, []cleanup.Candidate{candidate}, c.Sizes); err != nil {
			return err
		}
		if !c.DryRun {
			if err := g.Confirm("Remove " + candidate.ID + "?"); err != nil {
				if c.All && errors.Is(err, ErrRefused) {
					skipped++
					failures = append(failures, err)
					continue
				}
				return err
			}
		}
		result, err := cleanup.Apply(ctx, env, candidate, cleanup.ApplyOpts{DryRun: c.DryRun, IncludeIgnored: c.IncludeIgnored})
		if err != nil {
			failed++
			failures = append(failures, err)
			if !c.All {
				return err
			}
			if _, writeErr := fmt.Fprintf(out, "%s: failed: %s\n", candidate.ID, err); writeErr != nil {
				return writeErr
			}
			continue
		}
		if c.DryRun {
			skipped++
		} else {
			completed++
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
	return errors.Join(failures...)
}
func shellWord(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n\r'\"\\$`;|&<>()*?![]{}~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
func PrintCleanup(out io.Writer, cs []cleanup.Candidate, sizes bool) error {
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

type CleanupOutcome struct {
	ID     string            `json:"id"`
	Kind   cleanup.Kind      `json:"kind"`
	Status string            `json:"status"`
	Result *cleanup.Result   `json:"result,omitempty"`
	Error  *view.ErrorRecord `json:"error,omitempty"`
	Reason string            `json:"reason,omitempty"`
}

func (c CleanupOptions) JSONCandidates(ctx context.Context, env cleanup.Env, cs []cleanup.Candidate) ([]CleanupOutcome, error) {
	outcomes := []CleanupOutcome{}
	var failures []error
	for _, candidate := range cs {
		if c.ID != "" && c.ID != candidate.ID || c.Kind != "" && c.Kind != string(candidate.Kind) {
			continue
		}
		outcome := CleanupOutcome{ID: candidate.ID, Kind: candidate.Kind, Status: "unchanged"}
		switch {
		case ctx.Err() != nil:
			outcome.Status = "cancelled"
			outcome.Error = &view.ErrorRecord{Code: "cancelled", Message: ctx.Err().Error()}
			failures = append(failures, ctx.Err())
		case c.All && candidate.Status != "ready":
			outcome.Reason = candidate.Status
		case !c.DryRun && !c.Yes:
			err := InvalidUsage("not applied; pass --yes to apply without asking")
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
