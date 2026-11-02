// Package variant names which installation of Driga this build is: a
// release, a development build, or a copy under its own name. Each variant
// has its own service, log and default data directory, so a development
// build never shares the release's service, database or spool.
package variant

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// release is "true" in published, non-snapshot builds: .goreleaser.yaml sets
// it with -ldflags "-X github.com/labstack/driga/internal/variant.release=true".
// Every other build, including just build, go build, go run and a GoReleaser
// snapshot, is a development build.
var release string

var (
	mu       sync.Mutex
	override *string
)

// Suffix is what follows "driga" in this executable's name, "-dev" for a
// development build under the release's name, and empty for a release.
func Suffix() string {
	mu.Lock()
	o := override
	mu.Unlock()
	if o != nil {
		return *o
	}
	exe, err := os.Executable()
	if err != nil {
		exe = ""
	}
	return For(exe, release == "true")
}

// For is the suffix of the executable exe, built as a release or not.
func For(exe string, release bool) string {
	if v := Of(exe); v != "" || release {
		return v
	}
	return "-dev"
}

// Of is the suffix exe's own name carries: "-<name>" for driga-<name> or
// drigad-<name>, and empty otherwise.
func Of(exe string) string {
	name := strings.TrimSuffix(filepath.Base(exe), filepath.Ext(exe))
	for _, prefix := range []string{"drigad", "driga"} {
		if rest, ok := strings.CutPrefix(name, prefix); ok && (rest == "" || strings.HasPrefix(rest, "-")) {
			return rest
		}
	}
	return ""
}

// SetForTest makes Suffix return suffix until restore is called. Only tests
// call it; they run as development builds otherwise.
func SetForTest(suffix string) (restore func()) {
	mu.Lock()
	previous := override
	override = &suffix
	mu.Unlock()
	return func() {
		mu.Lock()
		override = previous
		mu.Unlock()
	}
}
