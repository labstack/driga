package cli

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"

	tea "charm.land/bubbletea/v2"
	"github.com/labstack/driga/internal/cleanup"
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	source, _, closeSource, err := openViewer(ctx, g, cfg, view.Filter{})
	if err != nil {
		return err
	}
	defer closeSource()
	opts := tui.Options{Machine: g.Machine, Project: g.Project, NoColor: g.NoColor, OpenCmd: cfg.OpenCmd, DataDir: config.DataDir(), Context: ctx}
	opts.CleanupList = func(ctx context.Context) ([]cleanup.Candidate, error) {
		envelope, err := cleanupRead(ctx, g, "", false, true)
		if err != nil {
			return nil, err
		}
		return envelope.Data.([]cleanup.Candidate), nil
	}
	opts.CleanupApply = func(ctx context.Context, c cleanup.Candidate) (cleanup.Result, error) {
		env, _, closeFn, err := openCleanup(ctx, g)
		if err != nil {
			return cleanup.Result{}, err
		}
		defer closeFn()
		return cleanup.Apply(ctx, env, c, cleanup.ApplyOpts{})
	}
	opts.Summarize = func(ctx context.Context, path string) error {
		local := *g
		local.Machine = "local"
		local.Project = ""
		local.JSON = false
		local.Stdout = io.Discard
		return (SummarizeCmd{Projects: []string{path}}).run(ctx, &local)
	}
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
