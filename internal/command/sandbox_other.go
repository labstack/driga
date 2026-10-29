//go:build !darwin && !linux && !windows

package command

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
)

func nativeCapability(context.Context) Capability {
	return Capability{OSVersion: runtime.GOOS, Reason: "Running commands needs a supported native sandbox."}
}
func sandboxProfile(Prepared) string { return "" }
func sandboxCommand(context.Context, Prepared, []string) (*exec.Cmd, func(), error) {
	return nil, func() {}, fmt.Errorf("sandbox unavailable")
}
