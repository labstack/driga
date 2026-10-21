package pathx

import (
	"os"
	"path/filepath"
	"runtime"
)

// TemporaryRecorded classifies a logged cwd by its path style. Only native
// paths may be resolved on this filesystem; foreign paths remain lexical.
func TemporaryRecorded(cwd string) bool {
	return temporaryRecorded(cwd, os.TempDir())
}

func temporaryRecorded(cwd, tempRoot string) bool {
	if !RecordedIsAbs(cwd) {
		return false
	}
	roots := []string{tempRoot}
	if !WindowsStyle(cwd) {
		roots = append(roots, "/tmp", "/private/tmp", "/var/tmp")
	}
	native := WindowsStyle(cwd) == (runtime.GOOS == "windows")
	if native {
		cwd = canonicalPath(cwd)
	}
	for _, root := range roots {
		if native && WindowsStyle(root) == (runtime.GOOS == "windows") {
			root = canonicalPath(root)
		}
		if RecordedWithin(cwd, root) {
			return true
		}
	}
	return false
}

func canonicalPath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return filepath.Clean(resolved)
	}
	// Logged directories may be gone; resolve their surviving ancestors.
	p = filepath.Clean(p)
	if parent := filepath.Dir(p); parent != p {
		return filepath.Join(canonicalPath(parent), filepath.Base(p))
	}
	return p
}
