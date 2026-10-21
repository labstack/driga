package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/logfile"
	"github.com/labstack/driga/internal/service"
	"github.com/labstack/driga/internal/store"
)

// CollectCmd runs the daemon, or one persistent pass with --once.
type CollectCmd struct {
	Once       bool          `help:"Collect once into the persistent database and exit."`
	CPUProfile string        `name:"cpu-profile" type:"path" help:"Write a CPU profile of the collector to this file."`
	ProfileFor time.Duration `name:"profile-for" default:"2m" help:"How long --cpu-profile records before it stops."`
}

// Run loads collection sources and handles graceful termination signals.
func (c CollectCmd) Run(g *Globals) error {
	applyCollectorMemoryLimit()
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
	if c.CPUProfile != "" {
		stopProfile, err := startCPUProfile(c.CPUProfile, c.ProfileFor)
		if err != nil {
			return err
		}
		defer stopProfile()
	}
	dir := config.DataDir()
	if !c.Once {
		restore, err := collectorLogging()
		if err != nil {
			return err
		}
		defer restore()
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
	if err := collector.MigrateSpool(ctx, d, st); err != nil {
		return err
	}
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

func collectorLogging() (func(), error) {
	w := &logfile.Writer{Path: service.LogPath()}
	if _, err := w.Write(nil); err != nil {
		return nil, err
	}
	var sink io.Writer = w
	if runtime.GOOS == "linux" {
		sink = io.MultiWriter(sink, os.Stderr)
	}
	previous := slog.Default()
	slog.SetDefault(slog.New(logfile.TextHandler(sink)))
	return func() { slog.SetDefault(previous) }, nil
}
