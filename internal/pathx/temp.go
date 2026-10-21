package pathx

import (
	"os"
	"runtime"
)

// TempRoots keeps historical Unix transcript classification unchanged.
func TempRoots() []string {
	if runtime.GOOS == "windows" {
		return []string{os.TempDir()}
	}
	return []string{os.TempDir(), "/tmp", "/private/tmp", "/var/tmp"}
}
