// Package self names this program's CLI executable. The same program runs
// as driga, the CLI, and as drigad, the Driga service; anything that starts
// Driga again, such as Chat's MCP server, a jump or an agent hook, must start
// the CLI, because a program started as drigad is the service.
package self

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Executable is the driga executable: this one, or the driga beside it when
// this one is drigad (drigad-dev runs driga-dev).
func Executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return executable(exe)
}

func executable(exe string) (string, error) {
	cli := CLI(exe)
	if cli == exe {
		return exe, nil
	}
	info, err := os.Stat(cli)
	if err != nil {
		return "", fmt.Errorf("Driga CLI beside service: %w", err)
	}
	if !info.Mode().IsRegular() || runtime.GOOS != "windows" && info.Mode().Perm()&0111 == 0 {
		return "", fmt.Errorf("Driga CLI beside service is not an executable file: %s", cli)
	}
	return cli, nil
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
