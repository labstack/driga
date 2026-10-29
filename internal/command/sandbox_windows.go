package command

import (
	"context"
	"fmt"
	"os/exec"
)

const windowsSandboxReason = "Running commands needs a sandbox, which Windows does not have yet."

func nativeCapability(context.Context) Capability {
	return Capability{OSVersion: "windows", Reason: windowsSandboxReason}
}
func sandboxProfile(Prepared) (string, error) { return "", nil }
func sandboxCommand(context.Context, Prepared, []string) (*exec.Cmd, func(), error) {
	return nil, func() {}, fmt.Errorf("%s", windowsSandboxReason)
}
