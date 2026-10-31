package cleanup

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// renameIntoTrash moves name from source into ~/.Trash. macOS privacy
// protection refuses to open ~/.Trash to a process without Full Disk Access,
// as the Driga service is, but lets it rename an item into it. So the move
// goes through a handle on the trash's parent, after checking that .Trash is
// a real directory on the same device, and never replaces an existing item.
func renameIntoTrash(source *os.File, name string, rootInfo os.FileInfo, destination string) error {
	trashDir := filepath.Dir(destination)
	parent, err := openDir(filepath.Dir(trashDir))
	if err != nil {
		return err
	}
	defer parent.Close()
	var st unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), filepath.Base(trashDir), &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR {
		return fmt.Errorf("trash directory is not a directory")
	}
	if int64(rootInfo.Sys().(*syscall.Stat_t).Dev) != int64(st.Dev) {
		return syscall.EXDEV
	}
	return unix.RenameatxNp(int(source.Fd()), name, int(parent.Fd()), filepath.Join(filepath.Base(trashDir), filepath.Base(destination)), unix.RENAME_EXCL)
}
