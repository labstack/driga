//go:build !windows

package cli

import (
	"context"
	"os/exec"
	"runtime"
)

func openWebBrowser(ctx context.Context, address string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	return exec.CommandContext(ctx, name, address).Run()
}
