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
func handOffApp(system bool, launchArgs ...string) (bool, error) {
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
	return true, handOffCommand(appBundle(exe), os.Environ(), launchArgs...).Run()
}

// handOffCommand opens bundle by its path: another copy of Driga.app, such
// as one in ~/Applications, has the same bundle identifier. Launch Services
// does not pass on the caller's environment, so the DRIGA_* variables naming
// the data, config and settings go along explicitly. A running app keeps
// its own.
func handOffCommand(bundle string, environ []string, launchArgs ...string) *exec.Cmd {
	args := []string{}
	for _, entry := range environ {
		if strings.HasPrefix(entry, "DRIGA_") && !strings.HasPrefix(entry, "DRIGA_APP=") {
			args = append(args, "--env", entry)
		}
	}
	args = append(args, "-a", bundle)
	if len(launchArgs) > 0 {
		args = append(args, "-n", "--args")
		args = append(args, launchArgs...)
	}
	return exec.Command("open", args...)
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
