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

func (h localLifecycle) controller() (Controller, error) { return New(h.opts.Executable) }
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
	cmd := exec.CommandContext(ctx, h.opts.Executable, "service", action)
	cmd.Env = h.opts.Environment
	out, err := platform.ChildOutput(cmd, true)
	if err != nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		return fmt.Errorf("driga service %s: %s", action, cmp.Or(lines[len(lines)-1], err.Error()))
	}
	return nil
}
