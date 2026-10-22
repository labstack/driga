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
	"strconv"
	"strings"
	"time"

	ks "github.com/kardianos/service"
	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/service"
)

// ServiceCmd controls the optional per-user collector.
type ServiceCmd struct {
	Action string `arg:"" enum:"install,uninstall,start,stop,restart,status,logs" help:"Service action: install, uninstall, start, stop, restart, status, logs."`
	Follow bool   `short:"f" help:"Follow collector log output (logs only)."`
}

// Run controls the user service or prints the last 50 collector log lines.
func (c ServiceCmd) Run(g *Globals) error {
	if c.Follow && c.Action != "logs" {
		return errors.New("-f is only supported by service logs")
	}
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
	bin, err = filepath.EvalSymlinks(bin)
	if err != nil {
		return err
	}
	serviceBin := stablePath(bin)
	if runtime.GOOS == "darwin" {
		serviceBin = service.ExecutablePath(serviceBin)
	}
	s, err := service.New(serviceBin)
	if err != nil {
		return err
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
		if err := s.Install(); err != nil {
			return err
		}
		if hint := service.InstallHint(runtime.GOOS); hint != "" {
			if _, err := fmt.Fprintln(commandOutput(g), hint); err != nil {
				return err
			}
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
		if task, ok := s.(interface{ TaskState() (string, error) }); ok {
			state, stateErr := task.TaskState()
			if stateErr != nil && !errors.Is(stateErr, ks.ErrNotInstalled) {
				return stateErr
			}
			held, err := probeCollectorLock(config.DataDir())
			if err != nil {
				return err
			}
			pidText, _ := os.ReadFile(filepath.Join(config.DataDir(), "driga.lock"))
			pid, _ := strconv.Atoi(strings.TrimSpace(string(pidText)))
			_, err = fmt.Fprintf(commandOutput(g), "task: %s; collector: lock held=%t, pid=%d, alive=%t\n", state, held, pid, collector.Running(config.DataDir()))
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
	_, err = fmt.Fprintln(commandOutput(g), "collector:", c.Action)
	return err
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
		name, args = "journalctl", []string{"--user", "-u", "run.driga.collector", "-n", "50"}
	}
	if follow {
		args = append(args, "-f")
	}
	if goos != "linux" {
		args = append(args, service.LogPath())
	}
	return name, args
}
