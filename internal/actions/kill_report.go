package actions

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/kill"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/scrub"
	"github.com/labstack/driga/internal/signatures"
	"github.com/labstack/driga/internal/view"
)

type KillOutcome struct {
	ID              string            `json:"id"`
	MachineID       string            `json:"machine_id"`
	RootPID         int32             `json:"root_pid"`
	Status          string            `json:"status"`
	Reason          string            `json:"reason,omitempty"`
	LastObservedCPU *float64          `json:"last_observed_cpu_percent,omitempty"`
	Result          *kill.Result      `json:"result,omitempty"`
	Error           *view.ErrorRecord `json:"error,omitempty"`
}

type KillReport struct {
	Outcomes  []KillOutcome      `json:"outcomes"`
	Completed int                `json:"completed"`
	Pending   int                `json:"pending"`
	Failed    int                `json:"failed"`
	Skipped   int                `json:"skipped"`
	Stopped   derive.MemoryUsage `json:"stopped_usage"`
	Warnings  []string           `json:"warnings,omitempty"`
}

// Execute serves the terminal and JSON commands through the same authorization,
// fresh verification and signal path. JSON callers discard the terminal preview.
func (c KillOptions) Execute(ctx context.Context, g Environment, cfg config.Config, procs []model.Proc, candidates []model.Orphan) (KillReport, error) {
	report := KillReport{Outcomes: []KillOutcome{}}
	sigs, err := signatures.Load(cfg.ExtraSignatures)
	if err != nil {
		return report, err
	}
	idx := derive.NewProcIndex(procs)
	classified := kill.WithClassifier(derive.NewClassifier(sigs, len(procs)))
	var errs []error
	for _, o := range candidates {
		started := time.Now().UTC()
		outcome, err := c.executeOne(ctx, g, cfg, idx, sigs, classified, o)
		report.Outcomes = append(report.Outcomes, outcome)
		if historyErr := recordKillOutcome(started, outcome); historyErr != nil {
			report.Warnings = append(report.Warnings, "Reclaim history could not be saved; the action outcome above still applies.")
		}
		switch outcome.Status {
		case "completed":
			report.Completed++
		case "pending", "partial":
			report.Pending++
		default:
			report.Failed++
		}
		if outcome.Result != nil {
			usage := outcome.Result.StoppedUsage
			report.Stopped.Processes += usage.Processes
			report.Stopped.Memory += usage.Memory
			report.Stopped.RSS += usage.RSS
			if usage.Processes > 0 {
				if report.Stopped.Measure == "" {
					report.Stopped.Measure = usage.Measure
				} else if report.Stopped.Measure != usage.Measure {
					report.Stopped.Measure = "mixed"
				}
			}
		}
		if err != nil {
			errs = append(errs, err)
			if !c.All {
				return report, errors.Join(errs...)
			}
			if _, writeErr := fmt.Fprintf(g.Stdout, "%s: %s\n", o.ID, err); writeErr != nil {
				return report, errors.Join(append(errs, writeErr)...)
			}
		}
	}
	return report, errors.Join(errs...)
}

func (c KillOptions) executeOne(ctx context.Context, g Environment, cfg config.Config, idx derive.ProcIndex, sigs *signatures.Set, classified kill.Option, o model.Orphan) (KillOutcome, error) {
	outcome := KillOutcome{ID: o.ID, MachineID: o.MachineID, RootPID: o.RootPID, Status: "failed"}
	fail := func(err error) (KillOutcome, error) {
		if outcome.Status == "completed" {
			outcome.Status = "failed"
		}
		if errors.Is(err, ErrRefused) || errors.Is(err, context.Canceled) {
			outcome.Status = "cancelled"
		}
		outcome.Error = &view.ErrorRecord{Code: "kill_failed", Message: scrub.Scrub(err.Error())}
		return outcome, err
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if o.Young {
		outcome.Status = "refused"
		return fail(fmt.Errorf("too young to kill (age %s, threshold %s)", max(time.Duration(0), time.Since(o.StartTime)).Round(time.Second), cfg.OrphanMinAge.Round(time.Second)))
	}
	pids := o.PIDs
	if len(pids) == 0 {
		pids = []int32{o.RootPID}
		for _, p := range idx.Descendants(o.RootPID) {
			pids = append(pids, p.PID)
		}
	}
	var expect []int32
	for _, pid := range pids {
		if pid != o.RootPID {
			expect = append(expect, pid)
		}
	}
	plat := g.Platform()
	verify := func(pin bool) error {
		if _, err := fmt.Fprintf(g.Stdout, "%s: checking it stays idle for %s…\n", o.ID, g.VerifyWindow); err != nil {
			return err
		}
		if pin {
			fresh, err := kill.VerifyForAction(ctx, plat, o, expect, g.VerifyWindow, classified)
			if err != nil {
				return err
			}
			o.ProcessStarts = fresh.ProcessStarts
			if err := verifyOrphanOwnership(ctx, g, plat, o); err != nil {
				return err
			}
		} else {
			ok, reason, err := kill.VerifyIdle(ctx, plat, o, expect, g.VerifyWindow, classified)
			if err != nil {
				return err
			}
			if !ok {
				return errors.New("not idle: " + reason)
			}
		}
		_, err := fmt.Fprintf(g.Stdout, "%s: verified idle\n", o.ID)
		return err
	}
	// Unverified snapshot candidates retain their eligibility preflight. Every
	// candidate gets a new, pinned verification after the consent below.
	if !o.Verified {
		if err := verify(false); err != nil {
			outcome.Status = "refused"
			return fail(err)
		}
	}
	command := "driga orphans kill " + o.ID
	if strings.HasPrefix(o.ID, "session:") {
		command = "driga agents kill " + strings.TrimPrefix(o.ID, "session:")
	}
	if c.Force {
		command += " --force"
	}
	if _, err := fmt.Fprintln(g.Stdout, command); err != nil {
		return fail(err)
	}
	if _, err := fmt.Fprintf(g.Stdout, "Directory: %q\n", o.Cwd); err != nil {
		return fail(err)
	}
	w := tabwriter.NewWriter(g.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "PID\tCMD\tMEMORY\tRSS\tCPU\tMEASURE")
	for _, pid := range pids {
		p, _ := idx.Get(pid)
		memory, measure := derive.ProcessMemory(p)
		fmt.Fprintf(w, "%d\t%q\t%s\t%s\t%.2f%%\t%s\n", pid, sigs.ProcessName(p.Cmdline, p.Exe, p.Argv), view.HumanBytes(memory), view.HumanBytes(p.RSS), p.CPUPercent, measure)
	}
	if err := w.Flush(); err != nil {
		return fail(err)
	}
	if members, known := derive.OrphanProcesses(idx, o); known {
		usage := derive.SumProcessMemory(members)
		cpu := 0.0
		for _, p := range members {
			cpu += p.CPUPercent
		}
		outcome.LastObservedCPU = &cpu
		if _, err := fmt.Fprintf(g.Stdout, "Target memory: %d bytes (%s); RSS %d bytes; last observed CPU %.2f%%\n", usage.Memory, usage.Measure, usage.RSS, cpu); err != nil {
			return fail(err)
		}
	} else if _, err := fmt.Fprintf(g.Stdout, "Target memory unavailable; stored RSS %d bytes\n", o.RSSTotal); err != nil {
		return fail(err)
	}
	if err := g.Confirm("Kill " + o.ID + "?"); err != nil {
		return fail(err)
	}
	if c.Force {
		if err := g.Confirm("Escalate survivors to SIGKILL (TerminateProcess on Windows)?"); err != nil {
			return fail(err)
		}
	}
	// A stored verified flag, or a preflight before the user deliberates, cannot
	// authorize signals. Fresh CPU/peer checks pin every member after all consent.
	if err := verify(true); err != nil {
		outcome.Status = "refused"
		return fail(err)
	}
	result, err := kill.Tree(ctx, plat, o, expect, c.Force, 5*time.Second, classified)
	outcome.Result = &result
	switch {
	case result.Aborted:
		outcome.Status = "refused"
		err = errors.Join(err, fmt.Errorf("kill %s aborted: %s", o.ID, result.Reason))
	case len(result.Unknown) > 0:
		outcome.Status = "unknown"
		err = errors.Join(err, fmt.Errorf("shutdown outcome unknown for PIDs %v", result.Unknown))
	case len(result.Survivors) > 0:
		outcome.Status = "pending"
		if len(result.Stopped) > 0 {
			outcome.Status = "partial"
		}
		err = errors.Join(err, fmt.Errorf("processes still running after shutdown: %v", result.Survivors))
	case err == nil:
		outcome.Status = "completed"
	}
	if _, writeErr := fmt.Fprintf(g.Stdout, "stopped %v; survivors %v; unknown %v\n", result.Stopped, result.Survivors, result.Unknown); writeErr != nil {
		err = errors.Join(err, writeErr)
	}
	if len(result.Stopped) > 0 {
		if _, writeErr := fmt.Fprintf(g.Stdout, "Exited processes previously held %d bytes (%s), including RSS %d bytes.\n", result.StoppedUsage.Memory, result.StoppedUsage.Measure, result.StoppedUsage.RSS); writeErr != nil {
			err = errors.Join(err, writeErr)
		}
	}
	if err != nil {
		return fail(err)
	}
	return outcome, nil
}
