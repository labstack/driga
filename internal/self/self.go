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

// CLI maps drigad to the CLI, in the outer Driga.app or beside an unbundled
// service executable. Other paths are unchanged.
func CLI(exe string) string {
	// The nested service has its own bundle identity. CLI clients must return
	// to the outer bundle rather than looking beside the helper executable.
	suffix := filepath.Join("Contents", "Helpers", "Driga Service.app", "Contents", "MacOS", "drigad")
	if strings.HasSuffix(exe, string(filepath.Separator)+suffix) {
		app := strings.TrimSuffix(exe, string(filepath.Separator)+suffix)
		if filepath.Base(app) == "Driga.app" {
			return filepath.Join(app, "Contents", "MacOS", "driga")
		}
	}
	dir, base := filepath.Split(exe)
	ext := filepath.Ext(base)
	if rest, ok := strings.CutPrefix(strings.TrimSuffix(base, ext), "drigad"); ok && (rest == "" || strings.HasPrefix(rest, "-")) {
		return filepath.Join(dir, "driga"+rest+ext)
	}
	return exe
}
