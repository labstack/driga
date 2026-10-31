package cleanup

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// renameIntoTrash moves name from source into ~/.Trash. macOS privacy
// protection refuses to open ~/.Trash to a process without Full Disk Access,
// as the Driga service is, but lets it rename an item into it. So the move
// goes through a handle on the trash's parent, refuses a symlink anywhere in
// the destination path, and never replaces an existing item. The kernel
// refuses a cross-device move with EXDEV.
func renameIntoTrash(source *os.File, name string, _ os.FileInfo, destination string) error {
	trashDir := filepath.Dir(destination)
	parent, err := openDir(filepath.Dir(trashDir))
	if err != nil {
		return err
	}
	defer parent.Close()
	return unix.RenameatxNp(int(source.Fd()), name, int(parent.Fd()), filepath.Join(filepath.Base(trashDir), filepath.Base(destination)), unix.RENAME_EXCL|unix.RENAME_NOFOLLOW_ANY)
}
