package pathx

import (
	"fmt"
	"path"
	"strings"
)

// WindowsStyle recognizes recorded Windows paths without consulting the host OS.
// A leading slash alone denotes a Unix path, even on a Windows collector.
func WindowsStyle(p string) bool {
	if strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//") {
		return false
	}
	return len(p) >= 2 && p[1] == ':' || strings.Contains(p, `\`) || strings.HasPrefix(p, "//")
}

func recordedOS(p string) string {
	if WindowsStyle(p) {
		return "windows"
	}
	return "unix"
}

// RecordedIsAbs accepts absolute agent/peer paths from either platform.
func RecordedIsAbs(p string) bool {
	if !WindowsStyle(p) {
		return path.IsAbs(p)
	}
	p = strings.ReplaceAll(p, `\`, "/")
	return strings.HasPrefix(p, "//") || len(p) >= 3 && p[1] == ':' && p[2] == '/'
}

// RecordedJoin leaves absolute agent paths exactly as recorded. Relative paths
// are resolved using the cwd's separators, never the collector's separators.
func RecordedJoin(cwd, p string) string {
	if RecordedIsAbs(p) || cwd == "" {
		return p
	}
	if !WindowsStyle(cwd) {
		return path.Join(cwd, p)
	}
	joined := recordedClean(strings.TrimRight(cwd, `/\`) + "/" + strings.ReplaceAll(p, `\`, "/"))
	if strings.Contains(cwd, `\`) {
		return strings.ReplaceAll(joined, "/", `\`)
	}
	return joined
}

// recordedClean preserves case and uses slashes for lexical operations.
func recordedClean(p string) string {
	if !WindowsStyle(p) {
		return path.Clean(p)
	}
	p = strings.ReplaceAll(p, `\`, "/")
	if strings.HasPrefix(p, "//?/") {
		p = p[4:]
		if len(p) >= 4 && strings.EqualFold(p[:4], "UNC/") {
			p = "//" + p[4:]
		}
	}
	if strings.HasPrefix(p, "//") {
		parts := strings.SplitN(p[2:], "/", 3)
		if len(parts) == 2 {
			return "//" + parts[0] + "/" + parts[1] + "/"
		}
		if len(parts) == 3 {
			return "//" + parts[0] + "/" + parts[1] + path.Clean("/"+parts[2])
		}
	}
	if len(p) >= 3 && p[1] == ':' && p[2] == '/' {
		return p[:2] + path.Clean(p[2:])
	}
	return path.Clean(p)
}

// RecordedKey is a comparison key, not a replacement for a stored/display path.
func RecordedKey(p string) string { return keyForOS(p, recordedOS(p)) }
func RecordedEqual(a, b string) bool {
	return WindowsStyle(a) == WindowsStyle(b) && RecordedKey(a) == RecordedKey(b)
}
func RecordedWithin(p, root string) bool {
	return WindowsStyle(p) == WindowsStyle(root) && withinForOS(p, root, recordedOS(root))
}

// RecordedRel returns a relative descendant path, preserving its recorded case.
func RecordedRel(root, p string) (string, error) {
	if !RecordedWithin(p, root) {
		return "", fmt.Errorf("recorded path is outside root")
	}
	root, p = recordedClean(root), recordedClean(p)
	if RecordedEqual(p, root) {
		return ".", nil
	}
	if root == "." {
		return p, nil
	}
	// Case folding can change UTF-8 byte lengths (for example, K and k).
	// Count components rather than slicing by the root's original byte length.
	parts := strings.Split(p, "/")
	depth := len(strings.Split(strings.TrimSuffix(root, "/"), "/"))
	return strings.Join(parts[depth:], "/"), nil
}

func RecordedBase(p string) string { return path.Base(recordedClean(p)) }
