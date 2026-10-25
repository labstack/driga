package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// startApp starts Driga.exe and lets it go; tests replace it.
var startApp = func(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

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
	return handOffTo(guiExecutable(exe))
}

// handOffTo starts the app executable app, detached from this console so
// Ctrl-C or closing the console leaves it running. An empty or missing app
// is a development build, which runs in place.
func handOffTo(app string) (bool, error) {
	if app == "" {
		return false, nil
	}
	if _, err := os.Stat(app); err != nil {
		return false, nil
	}
	cmd := exec.Command(app)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS}
	return true, startApp(cmd)
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
