package cli

import (
	"context"
	"github.com/shirou/gopsutil/v4/process"
	"golang.org/x/sys/windows"
)

func parentCommand(ctx context.Context, p *process.Process) (string, []string, error) {
	cmd, err := p.CmdlineWithContext(ctx)
	if err != nil {
		return "", nil, err
	}
	argv, err := windows.DecomposeCommandLine(cmd)
	return cmd, argv, err
}
