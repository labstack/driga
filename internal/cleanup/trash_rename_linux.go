package cleanup

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// renameIntoTrash moves name from source into the trash directory through a
// handle on that directory, so no component of it can be swapped for a
// symlink in between, and never replaces an existing item.
func renameIntoTrash(source *os.File, name string, rootInfo os.FileInfo, destination string) error {
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
	// Never replace an existing item. A filesystem without RENAME_NOREPLACE
	// falls back to the plain rename the free-name check guards.
	err = unix.Renameat2(int(source.Fd()), name, int(target.Fd()), filepath.Base(destination), unix.RENAME_NOREPLACE)
	if errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOSYS) {
		err = unix.Renameat(int(source.Fd()), name, int(target.Fd()), filepath.Base(destination))
	}
	return err
}
