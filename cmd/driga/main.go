// Command driga watches every AI coding agent session on this machine.
package main

import (
	"os"

	"github.com/labstack/driga/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], version, commit, date))
}
