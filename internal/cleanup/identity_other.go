//go:build !windows

package cleanup

import "os"

// Unix FileInfo contains the device and inode captured by Stat, eagerly.
type fileIdentity struct{ info os.FileInfo }

func captureIdentity(path string) (*fileIdentity, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return &fileIdentity{info: info}, nil
}
func sameIdentity(a, b *fileIdentity) bool {
	return a != nil && b != nil && os.SameFile(a.info, b.info)
}
