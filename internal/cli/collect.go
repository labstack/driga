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

// CollectCmd runs the daemon, or one persistent pass with --once.
type CollectCmd struct {
	Owner          string        `hidden:"" default:"manual" enum:"app,service,manual"`
	NotifyFD       *int          `hidden:""`
	ParentFD       *int          `hidden:""`
	Serve          *bool         `help:"Enable fleet serving alongside collection (overrides fleet.serve)."`
	Listen         string        `help:"Fleet private IP or interface and port (requires serving)."`
	Scheduled      bool          `hidden:""`
	ServiceEnv     []string      `hidden:"" sep:"none"`
	ServiceDataDir string        `hidden:""`
	Once           bool          `help:"Collect once into the persistent database and exit."`
	CPUProfile     string        `name:"cpu-profile" type:"path" help:"Write a CPU profile of the collector to this file."`
	ProfileFor     time.Duration `name:"profile-for" default:"2m" help:"How long --cpu-profile records before it stops."`
}

// Run loads collection sources and handles graceful termination signals.
func (c CollectCmd) Run(g *Globals) error {
	if c.NotifyFD != nil && *c.NotifyFD < 0 {
		return invalidUsage("--notify-fd must be non-negative")
	}
	if c.ParentFD != nil && *c.ParentFD < 0 {
		return invalidUsage("--parent-fd must be non-negative")
	}
	// Take the inherited descriptors first, before anything can start a
	// program that would inherit them too.
	var notifyFile, parentFile *os.File
	if c.NotifyFD != nil {
		var err error
		if notifyFile, err = inheritedFile(*c.NotifyFD, "notify"); err != nil {
			return invalidUsage("--notify-fd is not open")
		}
		defer notifyFile.Close()
	}
	if c.ParentFD != nil {
		var err error
		if parentFile, err = inheritedFile(*c.ParentFD, "parent"); err != nil {
			return invalidUsage("--parent-fd is not open")
		}
		defer parentFile.Close()
	}
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
	var notifier attention.Notifier
	if notifyFile != nil {
		notifier = &attention.PipeNotifier{W: notifyFile}
	}
	d, err := newCollectDeps(cfg, notifier)
	if err != nil {
		return err
	}
	d.Owner = collector.Owner{Kind: c.Owner}
	if g != nil {
		d.Owner.Version = g.Version
	}
	if runtime.GOOS == "darwin" && c.NotifyFD == nil {
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
	if parentFile != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()
		go watchParent(parentFile, cancel)
	}
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

// watchParent stops the collector when its parent goes away. The parent holds
// the write end of the pipe and never writes, so a read returns only once the
// parent closes it or exits; the collector then shuts down as on SIGTERM.
func watchParent(r io.Reader, cancel context.CancelFunc) {
	_, _ = io.Copy(io.Discard, r)
	slog.Info("parent process gone", "event", "parent_gone")
	cancel()
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
