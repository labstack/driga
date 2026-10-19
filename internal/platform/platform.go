// Package platform collects host process and machine snapshots.
package platform

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/driga/internal/model"
)

// ErrUnsupported means host collection is unavailable on this operating system.
var ErrUnsupported = errors.New("platform: unsupported operating system")

// Platform supplies process identity, machine health, and agent working directories.
type Platform interface {
	Procs(context.Context) ([]model.Proc, error)
	Machine(context.Context) (model.MachineSample, error)
	Cwds(context.Context, []int32) (map[int32]string, error)
	Alive(pid int32, start time.Time) bool
}

// SocketProber optionally checks socket activity for a candidate process tree.
// Results include every requested pid; true means an external peer or listener.
type SocketProber interface {
	ExternalPeers(ctx context.Context, pids []int32) (map[int32]bool, error)
}

// FileProber fails closed if open file descriptors cannot be inspected.
type FileProber interface {
	OpenFiles(context.Context, string) (bool, error)
}

// SameStart compares a kernel create time with a recorded process start.
// Registries may be written up to a minute after exec, or round a second early.
func SameStart(create, recorded time.Time) bool {
	delta := create.Sub(recorded)
	return delta >= -60*time.Second && delta <= time.Second
}

// ParsePressure extracts the system-wide free memory percentage from memory_pressure.
func ParsePressure(out []byte) (float64, error) {
	const prefix = "System-wide memory free percentage:"
	for _, line := range strings.Split(string(out), "\n") {
		value, ok := strings.CutPrefix(strings.TrimSpace(line), prefix)
		if !ok {
			continue
		}
		value, ok = strings.CutSuffix(strings.TrimSpace(value), "%")
		if !ok {
			return 0, errors.New("memory pressure percentage missing % suffix")
		}
		percent, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil || math.IsNaN(percent) || percent < 0 || percent > 100 {
			return 0, fmt.Errorf("invalid memory pressure percentage %q", value)
		}
		return percent, nil
	}
	return 0, errors.New("memory pressure percentage not found")
}

// ParseLsofCwd parses lsof -Fpn records, preserving spaces and Unicode in paths.
func ParseLsofCwd(out []byte) map[int32]string {
	cwds := make(map[int32]string)
	var pid int32
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case 'p':
			pid = 0
			if n, err := strconv.ParseInt(line[1:], 10, 32); err == nil && n > 0 {
				pid = int32(n)
			}
		case 'n':
			if pid > 0 {
				cwds[pid] = line[1:]
			}
		}
	}
	return cwds
}

// CPUPercent computes CPU use over dt; one fully used core is 100 percent.
func CPUPercent(prev, cur model.Proc, dt time.Duration) float64 {
	if dt <= 0 || prev.PID != cur.PID || !prev.StartTime.Equal(cur.StartTime) || cur.CPUSeconds < prev.CPUSeconds {
		return 0
	}
	return (cur.CPUSeconds - prev.CPUSeconds) / dt.Seconds() * 100
}

// SubreaperFinder is implemented by platforms where orphans can be reparented
// to a per-user child subreaper (Linux: the user's systemd --user instance)
// instead of pid 1.
type SubreaperFinder interface {
	UserSubreaper() (int32, bool)
}
