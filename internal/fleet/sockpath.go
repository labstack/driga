package fleet

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// maxSocketPath stays under macOS's 104-byte sun_path limit (Linux: 108).
const maxSocketPath = 100

// ControlSocketPath resolves the local control socket using the shared IPC rule.
func ControlSocketPath(dataDir string) string { return WatchSocketPath(dataDir, "fleet") }

// ensureSocketDir creates the socket's directory 0700 and refuses one that is
// a symlink or not owned by this user (the shared temp dir case).
func ensureSocketDir(sock string) error {
	if runtime.GOOS == "windows" {
		return ensureWindowsSocketDir(sock)
	}
	dir := filepath.Dir(sock)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return checkSocketDir(sock)
}

func checkSocketDir(sock string) error {
	if runtime.GOOS == "windows" {
		return checkWindowsSocketDir(sock)
	}
	dir := filepath.Dir(sock)
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || !ownedByMe(info) || info.Mode().Perm() != 0700 {
		return fmt.Errorf("fleet control dir %s must be a real directory owned by this user with mode 0700", dir)
	}
	return nil
}

func checkControlSocket(sock string) error {
	if err := checkSocketDir(sock); err != nil {
		return err
	}
	info, err := os.Lstat(sock)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 || !ownedByMe(info) {
		return fmt.Errorf("fleet control socket must be a socket owned by this user")
	}
	return nil
}

// WatchSocketPath prefers the data directory, then a private per-user directory
// under os.TempDir, then a short platform fallback. All names are stable across
// clients and collectors; the data-directory hash isolates independent instances.
// The conservative byte limit also fits Windows AF_UNIX's 108-byte sun_path.
func WatchSocketPath(dataDir, kind string) string {
	if abs, err := filepath.Abs(dataDir); err == nil {
		dataDir = abs
	}
	p := filepath.Join(dataDir, kind+".sock")
	if kind == "fleet" {
		p = filepath.Join(dataDir, "fleet", "fleet.sock")
	}
	if len(p) <= maxSocketPath {
		return p
	}
	sum := sha256.Sum256([]byte(filepath.Clean(dataDir)))
	name := kind + "-" + hex.EncodeToString(sum[:8]) + ".sock"
	p = filepath.Join(os.TempDir(), socketUserDir(), name)
	if len(p) <= maxSocketPath && filepath.IsAbs(p) {
		return p
	}
	return shortSocketPath(name)
}

// EnsureSocketDir and CheckSocketDir enforce the private IPC directory for both services.
func EnsureSocketDir(sock string) error { return ensureSocketDir(sock) }
func CheckSocketDir(sock string) error  { return checkSocketDir(sock) }
