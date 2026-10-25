package cli

import (
	"os"
	"syscall"
)

// inheritedFile opens a handle passed by the parent process, or reports that
// it is not open. Windows children inherit only the handles a process lists
// when it starts them.
func inheritedFile(fd int, name string) (*os.File, error) {
	if _, err := syscall.GetFileType(syscall.Handle(fd)); err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}
