package cli

import "os"

// inheritedFile opens a handle passed by the parent process. Windows children
// inherit only the handles a process lists when it starts them.
func inheritedFile(fd int, name string) *os.File { return os.NewFile(uintptr(fd), name) }
