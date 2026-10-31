package cleanup

import (
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// renameIntoTrash moves name from source into the trash directory through a
// handle on that directory, so no component of it can be swapped for a
// symlink in between.
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
	return unix.Renameat(int(source.Fd()), name, int(target.Fd()), filepath.Base(destination))
}
