package platform

import (
	"context"
	"fmt"
	"os/exec"
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
