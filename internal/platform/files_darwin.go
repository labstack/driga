package platform

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// OpenFiles asks lsof for all descriptors below the canonical directory.
func (darwinPlatform) OpenFiles(ctx context.Context, path string) (bool, error) {
	cmd := exec.CommandContext(ctx, "lsof", "-Fpn", "+D", path)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if stderr.Len() > 0 {
		return false, fmt.Errorf("open-file inspection: %s", stderr.String())
	}
	if err != nil {
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
			return false, err
		}
	}
	return len(out) > 0, nil
}

func (darwinPlatform) OpenFilePaths(ctx context.Context) ([]string, error) {
	out, err := exec.CommandContext(ctx, "lsof", "-Fn", "-w").Output()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 || len(out) != 0 {
			return nil, err
		}
	}
	return lsofOpenPaths(out)
}

func lsofOpenPaths(out []byte) ([]string, error) {
	paths := []string{}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(line, "n/") {
			continue
		}
		path, err := decodeLsofName(line[1:])
		if err != nil {
			return nil, fmt.Errorf("open-file inspection: %w", err)
		}
		path, err = resolveOpenPath(path)
		if err != nil {
			return nil, fmt.Errorf("open-file inspection: %w", err)
		}
		paths = append(paths, path)
	}
	return paths, nil
}

// lsof escapes names even with NUL field separators. C/hex escapes and doubled
// backslashes are reversible, but ^A can mean either two literal characters or
// a control byte. Refuse ambiguous evidence instead of guessing which file is open.
func decodeLsofName(name string) (string, error) {
	out := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c == '^' && i+1 < len(name) && (name[i+1] >= '@' && name[i+1] <= '_' || name[i+1] == '?') {
			return "", fmt.Errorf("ambiguous lsof control-character name")
		}
		if c == '\\' {
			i++
			if i >= len(name) {
				return "", fmt.Errorf("truncated lsof name escape")
			}
			switch name[i] {
			case '\\':
				c = '\\'
			case 'b':
				c = '\b'
			case 'f':
				c = '\f'
			case 'r':
				c = '\r'
			case 'n':
				c = '\n'
			case 't':
				c = '\t'
			case 'x':
				if i+2 >= len(name) {
					return "", fmt.Errorf("truncated lsof hex escape")
				}
				v, err := strconv.ParseUint(name[i+1:i+3], 16, 8)
				if err != nil {
					return "", fmt.Errorf("invalid lsof hex escape")
				}
				c = byte(v)
				i += 2
			default:
				return "", fmt.Errorf("unknown lsof name escape")
			}
		}
		if c == 0 {
			return "", fmt.Errorf("invalid NUL in lsof name")
		}
		out = append(out, c)
	}
	return string(out), nil
}

// An open name may vanish while lsof runs. Keep its canonical existing ancestor
// and lexical suffix so the descriptor still protects that directory; only a
// missing or inaccessible entry still supplies a name; other failures refuse.
func resolveOpenPath(path string) (string, error) {
	canonical, err := filepath.EvalSymlinks(path)
	if err == nil {
		return canonical, nil
	}
	parent := filepath.Dir(path)
	if (!os.IsNotExist(err) && !os.IsPermission(err)) || parent == path {
		return "", err
	}
	canonical, err = resolveOpenPath(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(canonical, filepath.Base(path)), nil
}
