package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// handOffApp starts Driga.exe when a command line opens the app from the
// installed CLI, bin\driga.exe, so the app runs apart from the console. A
// running app takes the launch over. It reports whether it handed off.
func handOffApp(system bool) (bool, error) {
	if system {
		return false, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	app := guiExecutable(exe)
	if app == "" {
		return false, nil
	}
	if _, err := os.Stat(app); err != nil {
		// A development build runs in place.
		return false, nil
	}
	cmd := exec.Command(app)
	if err := cmd.Start(); err != nil {
		return true, err
	}
	return true, cmd.Process.Release()
}

// guiExecutable returns the Driga.exe next to the bin directory of the CLI
// executable exe, or "" when exe is not in a bin directory.
func guiExecutable(exe string) string {
	dir := filepath.Dir(exe)
	if !strings.EqualFold(filepath.Base(dir), "bin") {
		return ""
	}
	return filepath.Join(filepath.Dir(dir), "Driga.exe")
}
