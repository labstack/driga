package scripts_test

import (
	"strings"
	"testing"
)

func TestLinuxDesktopEntry(t *testing.T) {
	entry := map[string]string{}
	for _, line := range strings.Split(readRepositoryFile(t, "../packaging/linux/driga.desktop"), "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			entry[key] = value
		}
	}
	for key, want := range map[string]string{
		"Type":           "Application",
		"Name":           "Driga",
		"Exec":           "driga app",
		"StartupWMClass": "run.driga",
		"Icon":           "run.driga",
		"Categories":     "Development;",
	} {
		if entry[key] != want {
			t.Errorf("%s = %q, want %q", key, entry[key], want)
		}
	}
}
