//go:build darwin || linux

package cleanup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// openDir walks the already canonical absolute path using directory handles.
// No component may turn into a symlink between verification and renameat.
func openDir(path string) (*os.File, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("directory must be absolute")
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Clean(path), "/"), "/") {
		if part == "" {
			continue
		}
		next, err := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if err != nil {
			return nil, err
		}
		fd = next
	}
	return os.NewFile(uintptr(fd), path), nil
}
func renameScratch(c Candidate, destination string) error {
	source, err := openDir(c.rootCanonical)
	if err != nil {
		return err
	}
	defer source.Close()
	rootInfo, err := source.Stat()
	if err != nil {
		return err
	}
	if c.rootIdentity == nil || !os.SameFile(rootInfo, c.rootIdentity) {
		return fmt.Errorf("scratch root identity changed")
	}
	name := filepath.Base(c.Path)
	fd, err := unix.Openat(int(source.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	leaf := os.NewFile(uintptr(fd), name)
	info, err := leaf.Stat()
	leaf.Close()
	if err != nil {
		return err
	}
	if !os.SameFile(info, c.identity) {
		return fmt.Errorf("scratch identity changed")
	}
	target, err := openDir(filepath.Dir(destination))
	if err != nil {
		return err
	}
	defer target.Close()
	targetInfo, err := target.Stat()
	if err != nil {
		return err
	}
	if !sameDevice(rootInfo.Sys().(*syscall.Stat_t), targetInfo.Sys().(*syscall.Stat_t)) {
		return syscall.EXDEV
	}
	return unix.Renameat(int(source.Fd()), name, int(target.Fd()), filepath.Base(destination))
}
