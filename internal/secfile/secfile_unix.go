//go:build !windows

package secfile

import (
	"errors"
	"fmt"
	"os"
)

func OpenFile(path string, flag int) (*os.File, error) {
	return os.OpenFile(path, flag, 0600)
}

func ProtectFile(f *os.File) error { return f.Chmod(0600) }

// MkdirPrivate retains MkdirAll's Unix behavior for existing directories.
func MkdirPrivate(path string) error { return os.MkdirAll(path, 0700) }

// Protect tightens an existing private file or directory.
func Protect(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	mode := os.FileMode(0600)
	if info.IsDir() {
		mode = 0700
	}
	return os.Chmod(path, mode)
}

// CheckPrivate retains the fleet key's existing group/world-read check.
func CheckPrivate(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode().Perm()&0044 != 0 {
		return fmt.Errorf("file is group/world readable; require mode 0600")
	}
	return nil
}

func createTemp(dir, pattern string) (*os.File, error) { return os.CreateTemp(dir, pattern) }

func syncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}
