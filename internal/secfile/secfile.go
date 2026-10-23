// Package secfile creates private storage using Unix modes or Windows DACLs.
package secfile

import (
	"errors"
	"os"
	"path/filepath"
)

// WritePrivate writes a file accessible only to its owner (and Windows system
// administrators). It preserves the usual os.WriteFile semantics on Unix.
func WritePrivate(path string, data []byte) error {
	f, err := OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	return errors.Join(err, f.Close())
}

// CreateTemp is os.CreateTemp with private permissions at creation, including
// on Windows. Callers own closing and removing the returned file.
func CreateTemp(dir, pattern string) (*os.File, error) {
	return createTemp(dir, pattern)
}

// WriteFileAtomic durably replaces path with a private file. check runs after
// the temporary file is closed and immediately before the atomic rename.
// The caller creates and validates the parent directory.
func WriteFileAtomic(path string, data []byte, check func() error) error {
	dir := filepath.Dir(path)
	f, err := CreateTemp(dir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(data)
	if err := errors.Join(writeErr, f.Sync(), f.Close()); err != nil {
		return err
	}
	if check != nil {
		if err := check(); err != nil {
			return err
		}
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	return syncDir(dir)
}
