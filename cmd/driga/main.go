// Command driga watches every AI coding agent session on this machine.
package main

import (
	"os"

	"github.com/labstack/driga/internal/cli"
	"github.com/labstack/driga/internal/service"
)

func main() {
	args := startupArgs(os.Args[0], os.Args[1:])
	notificationMain(args, func() { os.Exit(cli.Main(args, version, commit, date)) })
}

func startupArgs(executable string, args []string) []string {
	// Started as drigad, this program is the Driga service.
	if service.IsDaemon(executable) {
		return append([]string{"service", "run"}, args...)
	}
	if cli.AppLaunch(args) && len(args) == 0 {
		return []string{"app"}
	}
	return args
}
