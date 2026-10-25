package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/labstack/driga/internal/attention"
	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/fleet"
	"github.com/labstack/driga/internal/logfile"
	"github.com/labstack/driga/internal/service"
	"github.com/labstack/driga/internal/store"
)

// newCollectDeps builds the collector's sources; tests replace it so a full
// collect run reads no real machine state.
var newCollectDeps = newDeps

// ServiceRunCmd runs the Driga service in the foreground: what drigad runs,
// and what the login service starts. --once runs one pass and exits.
type ServiceRunCmd struct {
	Owner          string        `hidden:"" default:"manual" enum:"service,manual"`
	Serve          *bool         `help:"Enable fleet serving alongside collection (overrides fleet.serve)."`
	Listen         string        `help:"Fleet private IP or interface and port (requires serving)."`
	Scheduled      bool          `hidden:""`
	ServiceEnv     []string      `hidden:"" sep:"none"`
	ServiceDataDir string        `hidden:""`
	Once           bool          `help:"Collect once into the persistent database and exit."`
	CPUProfile     string        `name:"cpu-profile" type:"path" help:"Write a CPU profile of the service to this file."`
	ProfileFor     time.Duration `name:"profile-for" default:"2m" help:"How long --cpu-profile records before it stops."`
}

// Run loads collection sources and handles graceful termination signals.
func (c ServiceRunCmd) Run(g *Globals) error {
	if c.Once && c.Serve != nil && *c.Serve {
		return invalidUsage("--once and --serve cannot be combined")
	}
	if c.Scheduled {
		if err := service.PrepareScheduled(c.ServiceDataDir, c.ServiceEnv...); err != nil {
			return err
		}
	}
	applyCollectorMemoryLimit()
	if !c.Once {
		applyCollectorParallelism()
	}
	overrides := map[string]any{}
	if c.Serve != nil {
		overrides["fleet.serve"] = *c.Serve
	}
	if c.Listen != "" {
		overrides["fleet.listen"] = c.Listen
	}
	cfg, err := config.LoadWithOverrides(config.Path(), overrides)
	if err != nil {
		return err
	}
	if c.Once && cfg.Fleet.Serve {
		return invalidUsage("--once cannot serve fleet; pass --serve=false to override configuration")
	}
	if c.Listen != "" && !cfg.Fleet.Serve {
		return invalidUsage("--listen requires --serve")
	}
	if cfg.Fleet.Serve {
		if _, err := fleet.CheckListen(cfg.Fleet.Listen, cfg.Fleet.AllowAnyAddress); err != nil {
			return err
		}
	}
	d, err := newCollectDeps(cfg, nil)
	if err != nil {
		return err
	}
	d.Owner = collector.Owner{Kind: c.Owner}
	if g != nil {
		d.Owner.Version = g.Version
	}
	if runtime.GOOS == "darwin" {
		// Record what macOS reports, so a silent notification problem shows in the log.
		if status, err := attention.MacNotificationStatus(); err != nil {
			slog.Warn("macOS notifications unavailable", "event", "notifications_unavailable", "err", err.Error())
		} else {
			slog.Info("macOS notifications", "event", "notifications_status", "status", status)
		}
		attention.RequestNotificationPermission()
	}
	ctx, stop := notifyContext(context.Background())
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
		restore, err := collectorLogging(c.Owner)
		if err != nil {
			return err
		}
		defer restore()
		return collector.Run(ctx, d, dir)
	}
	lock, err := collector.Lock(dir, d.Owner)
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

// collectorLogging writes the service's log. On Linux the journal shows it too.
func collectorLogging(owner string) (func(), error) {
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
