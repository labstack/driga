// Package self names this program's CLI executable. The same program runs
// as driga, the CLI, and as drigad, the Driga service; anything that starts
// Driga again, such as Chat's MCP server, a jump or an agent hook, must start
// the CLI, because a program started as drigad is the service.
package self

import (
	"os"
	"path/filepath"
	"strings"
)

// Executable is the driga executable: this one, or the driga beside it when
// this one is drigad (drigad-dev runs driga-dev).
func Executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return CLI(exe), nil
}

// CLI maps a drigad path to the driga beside it; other paths are unchanged.
func CLI(exe string) string {
	dir, base := filepath.Split(exe)
	ext := filepath.Ext(base)
	if rest, ok := strings.CutPrefix(strings.TrimSuffix(base, ext), "drigad"); ok && (rest == "" || strings.HasPrefix(rest, "-")) {
		return filepath.Join(dir, "driga"+rest+ext)
	}
	return exe
}
