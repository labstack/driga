package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/store"
)

// CollectCmd runs the daemon, or one persistent pass with --once.
type CollectCmd struct {
	Once bool `help:"Collect once into the persistent database and exit."`
}

// Run loads collection sources and handles graceful termination signals.
func (c CollectCmd) Run(g *Globals) error {
	cfg, err := config.Load(config.Path())
	if err != nil {
		return err
	}
	d, err := NewDeps(cfg)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	dir := config.DataDir()
	if !c.Once {
		return collector.Run(ctx, d, dir)
	}
	lock, err := collector.Lock(dir)
	if err != nil {
		return err
	}
	defer lock.Close()
	st, err := store.Open(filepath.Join(dir, "driga.db"), store.ReadWrite)
	if err != nil {
		return err
	}
	defer st.Close()
	r, err := collector.Pass(ctx, d, st, collector.Plan{Procs: true, Machine: true, Git: true, GitFull: true, Cwds: true, Sockets: true})
	if err != nil {
		return err
	}
	var out io.Writer = os.Stdout
	if g != nil && g.Stdout != nil {
		out = g.Stdout
	}
	_, err = fmt.Fprintf(out, "Collected %d sessions, %d processes, %d samples (%d source errors).\n", r.Sessions, r.Procs, r.Samples, len(r.SourceErrors))
	return err
}
