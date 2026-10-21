package pathx

import (
	"net/url"
	"runtime"
	"strings"
)

// FileURL encodes an absolute host path as a SQLite-compatible file URI.
func FileURL(p string) string {
	return fileURLForOS(p, runtime.GOOS)
}
func fileURLForOS(p, goos string) string {
	if goos == "windows" {
		p = strings.ReplaceAll(p, `\`, "/")
		if len(p) > 1 && p[1] == ':' {
			p = "/" + p
		}
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}
