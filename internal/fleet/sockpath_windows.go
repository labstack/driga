package fleet

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/labstack/driga/internal/secfile"
	"golang.org/x/sys/windows"
)

func socketUserDir() string {
	user := os.Getenv("LOCALAPPDATA")
	if u, err := windows.GetCurrentProcessToken().GetTokenUser(); err == nil {
		user = u.User.Sid.String()
	}
	sum := sha256.Sum256([]byte(user))
	return "driga-" + hex.EncodeToString(sum[:4])
}

func shortSocketPath(name string) string {
	root := os.TempDir()
	// GetShortPathName helps when TEMP has a long profile component.
	if p, err := windows.UTF16PtrFromString(root); err == nil {
		buf := make([]uint16, 32768)
		if n, err := windows.GetShortPathName(p, &buf[0], uint32(len(buf))); err == nil && n < uint32(len(buf)) && n > 0 {
			short := windows.UTF16ToString(buf[:n])
			p := filepath.Join(short, socketUserDir(), name)
			if len(p) <= maxSocketPath {
				return p
			}
		}
	}
	// The system temp root stays short even with a long user TEMP/profile.
	// Creation and access still require the current user's private DACL.
	root, err := windows.GetWindowsDirectory()
	if err != nil {
		root = os.Getenv("SystemRoot")
	}
	return filepath.Join(root, "Temp", socketUserDir(), name)
}

func ensureWindowsSocketDir(sock string) error {
	dir := filepath.Dir(sock)
	if len(sock) >= 108 {
		return fmt.Errorf("Windows local socket path exceeds 107 bytes")
	}
	if _, err := os.Lstat(dir); os.IsNotExist(err) {
		if err := secfile.MkdirPrivate(dir); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return checkWindowsSocketDir(sock)
}

func checkWindowsSocketDir(sock string) error {
	dir := filepath.Dir(sock)
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("Windows socket parent must be a real private directory")
	}
	return secfile.CheckPrivate(dir)
}

// Windows AF_UNIX socket access is guarded by the checked private parent DACL.
func ownedByMe(os.FileInfo) bool                  { return true }
func protectWindowsDir(dir string) error          { return secfile.Protect(dir) }
func privateKey(path string, _ os.FileInfo) error { return secfile.CheckPrivate(path) }
func makePrivateFile(path string) error           { return secfile.Protect(path) }
