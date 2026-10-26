package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	ks "github.com/kardianos/service"
	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/pathx"
	"github.com/labstack/driga/internal/secfile"
)

var ErrIsolatedHome = errors.New("HOME is not the account's home; the login service is left alone")

// Status combines service registration with the observed lock and process.
// Running never relies on registration alone. The process outlives its clients.
type Status struct {
	Installed, LockHeld, Alive, Running bool
	Registration, TaskState             string
	Owner                               collector.Owner
	Err                                 error
	RestartPending                      bool
}

type Diagnostics struct {
	EnvironmentError error
	Shadowed         []string
	RestartPending   bool
}

// WithDiagnostics applies the same error precedence wherever status is shown.
func (s Status) WithDiagnostics(d Diagnostics) Status {
	switch {
	case d.RestartPending:
		return Status{RestartPending: true}
	case s.Err != nil:
	case d.EnvironmentError != nil && s.Running:
		s.Err = fmt.Errorf("without login shell PATH: %w", d.EnvironmentError)
	case len(d.Shadowed) > 0 && s.Running:
		s.Err = fmt.Errorf("ignoring %s from the login shell; quit Driga and run driga app from it", strings.Join(d.Shadowed, ", "))
	}
	return s
}

type LifecycleOptions struct {
	Executable, DataDir, Version, Platform string
	Home, AccountHome                      string
	Environment                            []string
	Output                                 io.Writer
}

// lifecycleHost has two adapters: the local OS and a local registration/process
// model in tests. Neither clients nor tests need to coordinate callback order.
type lifecycleHost interface {
	controller() (Controller, error)
	run(context.Context, string) error
	owner() (collector.Owner, bool, error)
	alive() bool
	prepareLog() error
	removeLegacy() error
}

// Lifecycle owns startup and observation. There is deliberately no shutdown
// method: canceling an app or CLI must leave drigad running independently.
type Lifecycle struct {
	opts LifecycleOptions
	host lifecycleHost
	mu   sync.Mutex
	err  error
}

func NewLifecycle(opts LifecycleOptions) *Lifecycle {
	if opts.Platform == "" {
		opts.Platform = runtime.GOOS
	}
	if opts.Output == nil {
		opts.Output = io.Discard
	}
	return &Lifecycle{opts: opts, host: localLifecycle{opts}}
}

// Ensure refreshes macOS registration, starts an existing registration elsewhere,
// and replaces a running older version. An isolated app never controls the
// account's service. Errors are retained in subsequent observations.
func (l *Lifecycle) Ensure(ctx context.Context) (err error) {
	defer func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.err = err
		if errors.Is(err, ErrIsolatedHome) {
			l.err = nil
		}
	}()
	if isolatedHome(l.opts.Home, l.opts.AccountHome) {
		return ErrIsolatedHome
	}
	action := "install"
	if l.opts.Platform != "darwin" {
		controller, err := l.host.controller()
		if err != nil {
			return err
		}
		exists, err := Installed(controller)
		if err != nil {
			return fmt.Errorf("query installed Driga service: %w", err)
		}
		if exists {
			action = "start"
		}
	}
	if err := l.host.run(ctx, action); err != nil {
		return err
	}
	owner, held, err := l.host.owner()
	if err != nil || !held || owner.Version == "" || owner.Version == l.opts.Version {
		return err
	}
	slog.Info("the Driga service runs another version; restarting it", "event", "service_restart", "running", owner.Version, "app", l.opts.Version)
	return l.host.run(ctx, "restart")
}

// Control implements explicit service administration, shared with the CLI.
// A failed install leaves the previous collector available.
func (l *Lifecycle) Control(ctx context.Context, action string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c, err := l.host.controller()
	if err != nil {
		return fmt.Errorf("initialize service controller: %w", err)
	}
	switch action {
	case "install":
		if err := l.host.prepareLog(); err != nil {
			return err
		}
		if err := c.Install(); err != nil {
			return err
		}
		if err := l.host.removeLegacy(); err != nil {
			return fmt.Errorf("remove the collector an earlier release installed: %w", err)
		}
		if hint := InstallHint(l.opts.Platform); hint != "" {
			if _, err := fmt.Fprintln(l.opts.Output, hint); err != nil {
				return err
			}
		}
		if err := c.Start(); err != nil {
			return fmt.Errorf("installed the Driga service; start it with driga service start: %w", err)
		}
		return nil
	case "uninstall":
		return c.Uninstall()
	case "start":
		return c.Start()
	case "stop":
		return c.Stop()
	case "restart":
		return c.Restart()
	default:
		return fmt.Errorf("unknown service action %q", action)
	}
}

func (l *Lifecycle) Observe() Status {
	s := Status{Registration: "unknown"}
	s.Owner, s.LockHeld, s.Err = l.host.owner()
	s.Alive = l.host.alive()
	s.Running = s.LockHeld && s.Alive
	c, err := l.host.controller()
	if err == nil {
		var status ks.Status
		status, err = c.Status()
		s.Installed = err == nil
		switch status {
		case ks.StatusRunning:
			s.Registration = "running"
		case ks.StatusStopped:
			s.Registration = "stopped"
		}
		if task, ok := c.(interface{ TaskState() (string, error) }); ok {
			s.TaskState, err = task.TaskState()
		}
	}
	if errors.Is(err, ks.ErrNotInstalled) {
		s.Registration, err = "not installed", nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if s.Err == nil {
		s.Err = l.err
	}
	if s.Err == nil {
		s.Err = err
	}
	return s
}

func isolatedHome(home, account string) bool {
	if home == "" {
		return false
	}
	resolve := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return filepath.Clean(p)
	}
	return account == "" || !pathx.Equal(resolve(home), resolve(account))
}

func prepareServiceLog() error {
	dir := filepath.Dir(LogPath())
	if err := secfile.MkdirPrivate(dir); err != nil {
		return err
	}
	if err := secfile.Protect(dir); err != nil {
		return err
	}
	f, err := secfile.OpenFile(LogPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY)
	if err != nil {
		return err
	}
	return errors.Join(secfile.ProtectFile(f), f.Close())
}
