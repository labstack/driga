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
)

type KillOptions struct{ All, Force bool }

func (c KillOptions) Kill(ctx context.Context, g Environment, cfg config.Config, procs []model.Proc, candidates []model.Orphan) error {
	idx := derive.NewProcIndex(procs)
	var errs []error
	for _, o := range candidates {
		if o.Young {
			err := fmt.Errorf("too young to kill (age %s, threshold %s)", max(time.Duration(0), time.Since(o.StartTime)).Round(time.Second), cfg.OrphanMinAge.Round(time.Second))
			if !c.All {
				return err
			}
			if _, err := fmt.Fprintf(g.Stdout, "%s: %s\n", o.ID, err); err != nil {
				return err
			}
			errs = append(errs, err)
			continue
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
		if !o.Verified {
			// One snapshot can't show idleness over time; check it now rather
			// than requiring the background service (spec §8.1 journey 5).
			if _, err := fmt.Fprintf(g.Stdout, "%s: checking it stays idle for %s…\n", o.ID, g.VerifyWindow); err != nil {
				return err
			}
			ok, reason, err := kill.VerifyIdle(ctx, g.Platform(), o, expect, g.VerifyWindow)
			if err == nil && !ok {
				err = errors.New("not idle: " + reason)
			}
			if err != nil {
				if !c.All {
					return err
				}
				if _, err := fmt.Fprintf(g.Stdout, "%s: %s\n", o.ID, err); err != nil {
					return err
				}
				errs = append(errs, err)
				continue
			}
			if _, err := fmt.Fprintf(g.Stdout, "%s: verified idle\n", o.ID); err != nil {
				return err
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
			return err
		}
		w := tabwriter.NewWriter(g.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "PID\tCMD\tRSS")
		for _, pid := range pids {
			p, _ := idx.Get(pid)
			fmt.Fprintf(w, "%d\t%s\t%d\n", pid, strings.Join(strings.Fields(p.Cmdline), " "), p.RSS)
		}
		if err := w.Flush(); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(g.Stdout, "~reclaim %d bytes (RSS estimate)\n", o.RSSTotal); err != nil {
			return err
		}
		if err := g.Confirm("Kill " + o.ID + "?"); err != nil {
			if c.All && errors.Is(err, ErrRefused) {
				errs = append(errs, err)
				continue
			}
			return err
		}
		if c.Force {
			if err := g.Confirm("Escalate survivors to SIGKILL (TerminateProcess on Windows)?"); err != nil {
				if c.All && errors.Is(err, ErrRefused) {
					errs = append(errs, err)
					continue
				}
				return err
			}
		}
		result, killErr := kill.Tree(ctx, g.Platform(), o, expect, c.Force, 5*time.Second)
		if result.Aborted {
			killErr = fmt.Errorf("kill %s aborted: %s", o.ID, result.Reason)
		}
		if _, err := fmt.Fprintf(g.Stdout, "signaled %v; survivors %v\n", result.Signaled, result.Survivors); err != nil {
			return err
		}
		if killErr != nil {
			errs = append(errs, killErr)
		}
	}
	return errors.Join(errs...)
}
