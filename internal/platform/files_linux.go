package platform

import (
	"context"
	"errors"
	"github.com/labstack/driga/internal/pathx"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func (linuxPlatform) OpenFiles(ctx context.Context, path string) (bool, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		dir := filepath.Join("/proc", entry.Name())
		info, err := os.Stat(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != uint32(os.Getuid()) {
			continue
		}
		fds, err := os.ReadDir(filepath.Join(dir, "fd"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(dir, "fd", fd.Name()))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return false, err
			}
			target = strings.TrimSuffix(target, " (deleted)")
			if pathx.Within(target, path) {
				return true, nil
			}
		}
	}
	return false, nil
}

func (linuxPlatform) OpenFilePaths(ctx context.Context) ([]string, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	paths := []string{}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		dir := filepath.Join("/proc", entry.Name(), "fd")
		fds, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			if ignorableFDPermission(filepath.Dir(dir), err) {
				continue
			}
			return nil, err
		}
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(dir, fd.Name()))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				if ignorableFDPermission(filepath.Dir(dir), err) {
					continue
				}
				return nil, err
			}
			target = strings.TrimSuffix(target, " (deleted)")
			if !filepath.IsAbs(target) {
				continue
			}
			if canonical, err := filepath.EvalSymlinks(target); err == nil {
				target = canonical
			}
			paths = append(paths, target)
		}
	}
	return paths, nil
}

// Only a positively identified foreign owner can be omitted. A nondumpable
// same-user process denies fd access too, and procfs inode ownership can become
// root for it, so read the process's Uid field instead of trusting Stat's owner.
func ignorableFDPermission(procDir string, err error) bool {
	if !errors.Is(err, os.ErrPermission) {
		return false
	}
	status, readErr := os.ReadFile(filepath.Join(procDir, "status"))
	if errors.Is(readErr, os.ErrNotExist) {
		return true // The process exited during inspection.
	}
	uid, known := procOwnerUID(status)
	return readErr == nil && known && uid != uint32(os.Getuid())
}
