package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/tui"
	"github.com/labstack/driga/internal/view"
)

// TopCmd opens the interactive viewer using daemon data or live snapshots.
type TopCmd struct{}

// Run keeps the viewer and its optional collector within the same lifetime.
func (TopCmd) Run(g *Globals) error {
	cfg, err := config.Load(config.Path())
	if err != nil {
		return err
	}
	since, err := parseSince(g.Since, time.Now())
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	st, _, err := decideMode(ctx, g)
	if err != nil {
		return err
	}
	f := view.Filter{Project: g.Project, Since: since}
	var source tui.Source
	if st != nil {
		defer st.Close()
		s := tui.NewStoreSource(st, config.DataDir(), f)
		s.ResolveProject = resolveProject
		source = s
	} else {
		d, err := NewDeps(cfg)
		if err != nil {
			return err
		}
		s, err := tui.NewLiveSnapshotSource(d, config.DataDir(), f)
		if err != nil {
			return err
		}
		s.ResolveProject = resolveProject
		s.Start(ctx)
		defer s.Close()
		source = s
	}
	opts := tui.Options{Project: g.Project, NoColor: g.NoColor, OpenCmd: cfg.OpenCmd, DataDir: config.DataDir(), Context: ctx}
	programOpts := []tea.ProgramOption{tea.WithContext(ctx)}
	if g.Stdin != nil {
		programOpts = append(programOpts, tea.WithInput(g.Stdin))
	}
	if g.Stdout != nil {
		programOpts = append(programOpts, tea.WithOutput(g.Stdout))
	}
	_, err = tea.NewProgram(tui.New(source, opts), programOpts...).Run()
	if ctx.Err() != nil {
		return nil
	}
	return err
}
