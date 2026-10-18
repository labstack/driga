package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	ks "github.com/kardianos/service"
	"github.com/labstack/driga/internal/service"
)

// ServiceCmd controls the optional per-user collector.
type ServiceCmd struct {
	Action string `arg:"" enum:"install,uninstall,start,stop,restart,status,logs" help:"Service action: install, uninstall, start, stop, restart, status, logs."`
	Follow bool   `short:"f" help:"Follow collector log output (logs only)."`
}

// Run controls launchd or prints the last 50 collector log lines.
func (c ServiceCmd) Run(g *Globals) error {
	if c.Follow && c.Action != "logs" {
		return errors.New("-f is only supported by service logs")
	}
	if c.Action == "logs" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		args := []string{"-n", "50"}
		if c.Follow {
			args = append(args, "-f")
		}
		args = append(args, service.LogPath())
		cmd := exec.CommandContext(ctx, "tail", args...)
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
	bin, err = filepath.EvalSymlinks(bin)
	if err != nil {
		return err
	}
	s, err := service.New(bin)
	if err != nil {
		return err
	}
	switch c.Action {
	case "install":
		// launchd opens these streams before the collector starts.
		dir := filepath.Dir(service.LogPath())
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		if err := os.Chmod(dir, 0700); err != nil {
			return err
		}
		f, err := os.OpenFile(service.LogPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		if err := errors.Join(f.Chmod(0600), f.Close()); err != nil {
			return err
		}
		if err := s.Install(); err != nil {
			return err
		}
		if err := s.Start(); err != nil {
			return fmt.Errorf("installed collector; start with driga service start: %w", err)
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
	_, err = fmt.Fprintln(commandOutput(g), "collector:", c.Action)
	return err
}

func commandOutput(g *Globals) io.Writer {
	if g != nil && g.Stdout != nil {
		return g.Stdout
	}
	return io.Discard
}
