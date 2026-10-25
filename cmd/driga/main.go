// Command driga watches every AI coding agent session on this machine.
package main

import (
	"os"

	"github.com/labstack/driga/internal/cli"
	"github.com/labstack/driga/internal/service"
)

func main() {
	args := os.Args[1:]
	// Started as drigad, this program is the Driga service.
	if service.IsDaemon(os.Args[0]) {
		args = append([]string{"service", "run"}, args...)
	} else if cli.AppLaunch(args) {
		args = []string{"app"}
	}
	notificationMain(args, func() { os.Exit(cli.Main(args, version, commit, date)) })
}
