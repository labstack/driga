// Command driga watches every AI coding agent session on this machine.
package main

import (
	"os"

	"github.com/labstack/driga/internal/cli"
)

// version is set at build time via -ldflags "-X main.version=…".
var version = "dev"

func main() {
	os.Exit(cli.Main(os.Args[1:], version))
}
