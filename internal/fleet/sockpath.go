package fleet

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// maxSocketPath stays under macOS's 104-byte sun_path limit (Linux: 108).
const maxSocketPath = 100

// ControlSocketPath is where the collector's control socket lives for dataDir:
// fleet/fleet.sock in the data dir, or, when that path is too long for a Unix
// socket, a short per-user directory keyed by a hash of dataDir.
func ControlSocketPath(dataDir string) string {
	p := filepath.Join(dataDir, "fleet", "fleet.sock")
	if len(p) <= maxSocketPath {
		return p
	}
	sum := sha256.Sum256([]byte(filepath.Clean(dataDir)))
	name := hex.EncodeToString(sum[:8]) + ".sock"
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); filepath.IsAbs(runtimeDir) {
		p := filepath.Join(runtimeDir, "driga", name)
		if len(p) <= maxSocketPath {
			return p
		}
	}
	return filepath.Join("/tmp", fmt.Sprintf("driga-%d", os.Getuid()), name)
}

// ensureSocketDir creates the socket's directory 0700 and refuses one that is
// a symlink or not owned by this user (the shared temp dir case).
func ensureSocketDir(sock string) error {
	dir := filepath.Dir(sock)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return checkSocketDir(sock)
}

func checkSocketDir(sock string) error {
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
