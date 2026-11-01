package platform

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// OpenFiles asks lsof for all descriptors below the canonical directory.
func (darwinPlatform) OpenFiles(ctx context.Context, path string) (bool, error) {
	cmd := exec.CommandContext(ctx, "lsof", "-Fpn", "+D", path)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if stderr.Len() > 0 {
		return false, fmt.Errorf("open-file inspection: %s", stderr.String())
	}
	if err != nil {
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
			return false, err
		}
	}
	return len(out) > 0, nil
}

func (darwinPlatform) OpenFilePaths(ctx context.Context) ([]string, error) {
	out, err := exec.CommandContext(ctx, "lsof", "-Fn", "-w").Output()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 || len(out) != 0 {
			return nil, err
		}
	}
	paths := []string{}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(line, "n/") {
			continue
		}
		if path, err := filepath.EvalSymlinks(line[1:]); err == nil {
			paths = append(paths, path)
		}
	}
	return paths, nil
}
