package service

import (
	"cmp"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/platform"
)

type localLifecycle struct{ opts LifecycleOptions }

func (h localLifecycle) controller() (Controller, error) {
	c, err := New(h.opts.Executable)
	if r, ok := c.(interface{ refreshEnvironment() }); ok && h.opts.RefreshEnvironment {
		r.refreshEnvironment()
	}
	return c, err
}
func (h localLifecycle) owner() (collector.Owner, bool, error) {
	return collector.ReadOwner(h.opts.DataDir)
}
func (h localLifecycle) alive() bool { return collector.Running(h.opts.DataDir) }
func (h localLifecycle) prepareLog() error {
	if h.opts.Platform == "darwin" {
		return prepareServiceLog()
	}
	return nil
}
func (h localLifecycle) removeLegacy() error { return RemoveLegacy() }

func (h localLifecycle) run(ctx context.Context, action string) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	args := []string{"service", action}
	if action == "install" {
		// The app's login-shell environment is the one the service records.
		args = append(args, "--refresh-env")
	}
	cmd := exec.CommandContext(ctx, h.opts.Executable, args...)
	cmd.Env = h.opts.Environment
	out, err := platform.ChildOutput(cmd, true)
	if err != nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		return fmt.Errorf("driga service %s: %s", action, cmp.Or(lines[len(lines)-1], err.Error()))
	}
	return nil
}
