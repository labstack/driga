package cleanup

import (
	"fmt"
	"golang.org/x/sys/unix"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

func trashDirs(home string) (string, string, error) {
	root := filepath.Join(home, ".local/share/Trash")
	return filepath.Join(root, "files"), filepath.Join(root, "info"), nil
}
func trashInfo(dir, name, path string, at time.Time) (func(bool), error) {
	parent, err := openDir(dir)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Openat(int(parent.Fd()), name+".trashinfo", unix.O_CREAT|unix.O_EXCL|unix.O_WRONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		parent.Close()
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name+".trashinfo")
	finish := func(success bool) {
		if !success {
			_ = unix.Unlinkat(int(parent.Fd()), name+".trashinfo", 0)
		}
		_ = parent.Close()
	}
	escaped := (&url.URL{Path: path}).EscapedPath()
	_, err = fmt.Fprintf(f, "[Trash Info]\nPath=%s\nDeletionDate=%s\n", escaped, at.Local().Format("2006-01-02T15:04:05"))
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		finish(false)
		return nil, err
	}
	return finish, nil
}
