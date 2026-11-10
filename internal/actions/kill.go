package actions

import (
	"context"
	"errors"
	"fmt"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/model"
)

type KillOptions struct{ All, Force bool }

func (c KillOptions) Kill(ctx context.Context, g Environment, cfg config.Config, procs []model.Proc, candidates []model.Orphan) error {
	report, err := c.Execute(ctx, g, cfg, procs, candidates)
	if len(report.Outcomes) > 0 {
		_, writeErr := fmt.Fprintf(g.Stdout, "completed %d trees; pending %d; failed %d; stopped %d processes\n", report.Completed, report.Pending, report.Failed, report.Stopped.Processes)
		err = errors.Join(err, writeErr)
	}
	for _, warning := range report.Warnings {
		_, writeErr := fmt.Fprintln(g.Stdout, warning)
		err = errors.Join(err, writeErr)
	}
	return err
}
