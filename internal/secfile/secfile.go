// Package secfile creates private storage using Unix modes or Windows DACLs.
package secfile

import (
	"errors"
	"os"
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
