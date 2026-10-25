package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// handOffApp opens Driga.app through Launch Services when a command line
// starts the app from its bundle, so the app runs with the bundle's identity
// for its login item and notifications. It reports whether it handed off.
func handOffApp(system bool) (bool, error) {
	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	// Homebrew links the bundle's binary into its bin directory.
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	if !handsOff(system, exe) {
		return false, nil
	}
	return true, exec.Command("open", "-b", "run.driga").Run()
}

// handsOff reports whether to hand off: a launch not made by the system, of
// an executable inside an app bundle. A development build runs in place.
func handsOff(system bool, exe string) bool {
	return !system && appBundle(exe) != ""
}

// appBundle returns the .app bundle whose executable exe is, or "".
func appBundle(exe string) string {
	macOS := filepath.Dir(exe)
	contents := filepath.Dir(macOS)
	bundle := filepath.Dir(contents)
	if filepath.Base(macOS) != "MacOS" || filepath.Base(contents) != "Contents" || !strings.HasSuffix(bundle, ".app") {
		return ""
	}
	return bundle
}
