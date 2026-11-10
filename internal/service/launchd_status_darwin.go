package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	ks "github.com/kardianos/service"
	"github.com/labstack/driga/internal/platform"
)

const launchdStatusTimeout = 5 * time.Second

var launchdStatusPID = regexp.MustCompile(`(?m)^\s*"PID"\s*=\s*([0-9]+);`)

// launchdCapture drains both pipes without retaining unbounded job metadata.
type launchdCapture struct {
	bytes.Buffer
	limit     int
	truncated bool
}

func (b *launchdCapture) Write(p []byte) (int, error) {
	n := len(p)
	remaining := b.limit - b.Len()
	if len(p) > remaining {
		p = p[:remaining]
		b.truncated = true
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}

// Status avoids the upstream sequential stderr/stdout reader. A full stdout
// pipe otherwise prevents launchctl exiting and stderr ever reaching EOF.
func (s *launchdController) Status() (ks.Status, error) {
	// In-memory controllers used by the lifecycle model supply their own status.
	if s.statusCommand == nil {
		return s.Controller.Status()
	}
	ctx, cancel := context.WithTimeout(context.Background(), launchdStatusTimeout)
	defer cancel()
	cmd := s.statusCommand(ctx, "list", Label("darwin"))
	stdout := launchdCapture{limit: 1 << 20}
	stderr := launchdCapture{limit: 64 << 10}
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := platform.RunChild(cmd)
	if ctx.Err() != nil {
		return ks.StatusUnknown, fmt.Errorf("query Driga launch agent: %w", ctx.Err())
	}
	if stdout.truncated || stderr.truncated {
		return ks.StatusUnknown, errors.New("launchctl status response exceeds limit")
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && (exit.ExitCode() == 3 || exit.ExitCode() == 113) {
			if _, statErr := os.Stat(s.plistPath); statErr == nil {
				return ks.StatusStopped, nil
			} else if errors.Is(statErr, os.ErrNotExist) {
				return ks.StatusUnknown, ks.ErrNotInstalled
			} else {
				return ks.StatusUnknown, statErr
			}
		}
		// Job output can contain environment values; never include it in errors.
		return ks.StatusUnknown, fmt.Errorf("query Driga launch agent: %w", err)
	}
	response := strings.TrimSpace(stdout.String())
	if !strings.HasPrefix(response, "{") || !strings.HasSuffix(response, "};") {
		return ks.StatusUnknown, errors.New("unexpected launchctl status response")
	}
	if match := launchdStatusPID.FindStringSubmatch(response); len(match) == 2 {
		pid, err := strconv.Atoi(match[1])
		if err != nil {
			return ks.StatusUnknown, errors.New("invalid launchctl service PID")
		}
		if pid > 0 {
			return ks.StatusRunning, nil
		}
	}
	return ks.StatusStopped, nil
}
