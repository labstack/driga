//go:build unix

package fleet

import (
	"fmt"
	"github.com/labstack/driga/internal/secfile"
	"os"
	"syscall"
)

func ownedByMe(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}

func ensureWindowsSocketDir(sock string) error { return ensureSocketDir(sock) }
func checkWindowsSocketDir(sock string) error  { return checkSocketDir(sock) }

func protectWindowsDir(dir string) error { return os.Chmod(dir, 0700) }
func privateKey(path string, _ os.FileInfo) error {
	if err := secfile.CheckPrivate(path); err != nil {
		return fmt.Errorf("fleet identity key is group/world readable; require mode 0600")
	}
	return nil
}

func makePrivateFile(string) error { return nil }

// Only the Windows socket path resolver needs a short temporary fallback.
func shortSocketPath(name string) string { return name }
