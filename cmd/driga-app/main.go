//go:build windows

// Command driga-app is Driga.exe, the Windows desktop app, built with
// -ldflags -H=windowsgui. Its collector is the CLI next to it, bin\driga.exe.
package main

import (
	"os"
	"path/filepath"

	"github.com/labstack/driga/internal/cli"
)

// version, commit and date are set like the CLI's, via -ldflags "-X main.version=…".
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	exe, err := os.Executable()
	if err != nil {
		os.Exit(1)
	}
	os.Exit(cli.AppMain(filepath.Join(filepath.Dir(exe), "bin", "driga.exe"), version, commit, date))
}
