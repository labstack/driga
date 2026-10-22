package pathx

import (
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
)

// FileURL resolves a host path and encodes it as a SQLite-compatible file URI.
func FileURL(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return fileURLForOS(filepath.Clean(p), runtime.GOOS)
}
func fileURLForOS(p, goos string) string {
	if goos == "windows" {
		p = strings.ReplaceAll(p, `\`, "/")
		if len(p) > 1 && p[1] == ':' {
			p = "/" + p
		}
	}
	if !strings.HasPrefix(p, "/") {
		// Abs can fail when the working directory has been removed. Keep any
		// relative fallback opaque so its first component cannot become a host.
		return "file:" + (&url.URL{Path: p}).EscapedPath()
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}
