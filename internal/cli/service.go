package cli

import (
	"context"
	"errors"
	"fmt"
	"github.com/labstack/driga/internal/secfile"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	ks "github.com/kardianos/service"
	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/service"
)

// ServiceCmd runs and controls the Driga service, drigad: the one process
// per machine that collects, stores, hosts Chat and runs actions.
type ServiceCmd struct {
	Run       ServiceRunCmd       `cmd:"" help:"Run the Driga service in the foreground (what drigad runs)."`
	Install   ServiceInstallCmd   `cmd:"" help:"Install the Driga service as a login service and start it."`
	Start     ServiceStartCmd     `cmd:"" help:"Start the Driga service."`
	Stop      ServiceStopCmd      `cmd:"" help:"Stop the Driga service."`
	Restart   ServiceRestartCmd   `cmd:"" help:"Restart the Driga service."`
	Status    ServiceStatusCmd    `cmd:"" help:"Show whether the Driga service runs."`
	Logs      ServiceLogsCmd      `cmd:"" help:"Print the Driga service's log."`
	Uninstall ServiceUninstallCmd `cmd:"" help:"Uninstall the Driga service."`
}
type ServiceInstallCmd struct{}

func (ServiceInstallCmd) Run(g *Globals) error { return (serviceControl{Action: "install"}).Run(g) }

type ServiceStartCmd struct{}

func (ServiceStartCmd) Run(g *Globals) error { return (serviceControl{Action: "start"}).Run(g) }

type ServiceStopCmd struct{}

func (ServiceStopCmd) Run(g *Globals) error { return (serviceControl{Action: "stop"}).Run(g) }

type ServiceRestartCmd struct{}

func (ServiceRestartCmd) Run(g *Globals) error { return (serviceControl{Action: "restart"}).Run(g) }

type ServiceStatusCmd struct{}

func (ServiceStatusCmd) Run(g *Globals) error { return (serviceControl{Action: "status"}).Run(g) }

type ServiceLogsCmd struct {
	Follow bool `short:"f" help:"Follow the log."`
}

func (c ServiceLogsCmd) Run(g *Globals) error {
	return (serviceControl{Action: "logs", Follow: c.Follow}).Run(g)
}

type ServiceUninstallCmd struct{}

func (ServiceUninstallCmd) Run(g *Globals) error { return (serviceControl{Action: "uninstall"}).Run(g) }

type serviceControl struct {
	Action string
	Follow bool
}

// Run controls the login service or prints the last 50 lines of its log.
func (c serviceControl) Run(g *Globals) error {
	if c.Action == "logs" {
		ctx, stop := notifyContext(context.Background())
		defer stop()
		if runtime.GOOS == "windows" {
			return streamServiceLog(ctx, service.LogPath(), c.Follow, commandOutput(g))
		}
		name, args := serviceLogCommand(runtime.GOOS, c.Follow)
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Stdout, cmd.Stderr = commandOutput(g), g.Stderr
		err := cmd.Run()
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	serviceBin, err := serviceBinaryPath(bin)
	if err != nil {
		return fmt.Errorf("resolve service executable %q: %w", bin, err)
	}
	if runtime.GOOS == "darwin" {
		serviceBin = service.ExecutablePath(serviceBin)
	}
	s, err := service.New(serviceBin)
	if err != nil {
		return fmt.Errorf("initialize service controller: %w", err)
	}
	switch c.Action {
	case "install":
		if runtime.GOOS == "darwin" {
			// launchd opens these streams before the collector starts.
			dir := filepath.Dir(service.LogPath())
			if err := secfile.MkdirPrivate(dir); err != nil {
				return err
			}
			if err := secfile.Protect(dir); err != nil {
				return err
			}
			f, err := secfile.OpenFile(service.LogPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY)
			if err != nil {
				return err
			}
			if err := errors.Join(secfile.ProtectFile(f), f.Close()); err != nil {
				return err
			}
		}
		if err := service.RemoveLegacy(); err != nil {
			return fmt.Errorf("remove the collector an earlier release installed: %w", err)
		}
		if err := s.Install(); err != nil {
			return err
		}
		if hint := service.InstallHint(runtime.GOOS); hint != "" {
			if _, err := fmt.Fprintln(commandOutput(g), hint); err != nil {
				return err
			}
		}
		if err := s.Start(); err != nil {
			return fmt.Errorf("installed the Driga service; start it with driga service start: %w", err)
		}
	case "uninstall":
		err = s.Uninstall()
	case "start":
		err = s.Start()
	case "stop":
		err = s.Stop()
	case "restart":
		err = s.Restart()
	case "status":
		if task, ok := s.(interface{ TaskState() (string, error) }); ok {
			state, stateErr := task.TaskState()
			if stateErr != nil && !errors.Is(stateErr, ks.ErrNotInstalled) {
				return fmt.Errorf("query the service task: %w", stateErr)
			}
			owner, held, err := collector.ReadOwner(config.DataDir())
			if err != nil {
				return fmt.Errorf("probe the service lock: %w", err)
			}
			_, err = fmt.Fprintf(commandOutput(g), "task: %s; service: lock held=%t, pid=%d, alive=%t\n", state, held, owner.PID, collector.Running(config.DataDir()))
			return err
		}
		status, statusErr := s.Status()
		if statusErr != nil && !errors.Is(statusErr, ks.ErrNotInstalled) {
			return statusErr
		}
		label := "unknown"
		switch status {
		case ks.StatusRunning:
			label = "running"
		case ks.StatusStopped:
			label = "stopped"
		}
		if errors.Is(statusErr, ks.ErrNotInstalled) {
			label = "not installed"
		}
		_, err = fmt.Fprintln(commandOutput(g), label)
		return err
	default:
		return fmt.Errorf("unknown service action %q", c.Action)
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(commandOutput(g), "Driga service:", c.Action)
	return err
}

// collectorHint is the command that starts the Driga service. The app
// installs it on a desktop; anywhere, this does.
func collectorHint(string) string { return "driga service install" }

// restartHint says how to restart the collector that holds the lock: the
// app, the service or a collector run by hand. With none holding it, the
// collector is started the way this platform runs it.
func restartHint(goos string, owner collector.Owner, held bool) string {
	if !held {
		return collectorHint(goos)
	}
	switch owner.Kind {
	case "service":
		return "driga service restart"
	case "manual":
		return "stop and rerun driga service run"
	}
	return "driga service restart"
}

func serviceBinaryPath(bin string) (string, error) {
	if runtime.GOOS == "windows" {
		// Keep Scoop's stable current junction. The OS can execute this path
		// even when filepath.EvalSymlinks cannot walk the reparse point, and
		// persisting a version-specific target would break after an upgrade.
		return filepath.Abs(bin)
	}
	resolved, err := filepath.EvalSymlinks(bin)
	if err != nil {
		return "", err
	}
	return stablePath(resolved), nil
}

// Native log tail for Windows; no dependency on Unix tail or PowerShell.
func streamServiceLog(ctx context.Context, path string, follow bool, out io.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	start := max(int64(0), info.Size()-(256<<10))
	b, err := io.ReadAll(io.NewSectionReader(f, start, info.Size()-start))
	if err != nil {
		return err
	}
	lines := strings.SplitAfter(string(b), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if _, err := io.WriteString(out, strings.Join(lines[max(0, len(lines)-50):], "")); err != nil {
		return err
	}
	if !follow {
		return nil
	}
	pos := info.Size()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
		info, err = f.Stat()
		if err != nil {
			return err
		}
		if info.Size() < pos {
			pos = 0
		}
		n, err := io.Copy(out, io.NewSectionReader(f, pos, max(0, info.Size()-pos)))
		pos += n
		if err != nil {
			return err
		}
	}
}

func commandOutput(g *Globals) io.Writer {
	if g != nil && g.Stdout != nil {
		return g.Stdout
	}
	return io.Discard
}

func serviceLogCommand(goos string, follow bool) (string, []string) {
	name, args := "tail", []string{"-n", "50"}
	if goos == "linux" {
		name, args = "journalctl", []string{"--user", "-u", service.Label("linux"), "-n", "50"}
	}
	if follow {
		args = append(args, "-f")
	}
	if goos != "linux" {
		args = append(args, service.LogPath())
	}
	return name, args
}
