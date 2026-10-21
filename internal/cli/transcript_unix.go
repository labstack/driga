//go:build !windows

package cli

import (
	"os"
	"syscall"
)

func openTranscript(p string) (*os.File, error) {
	return os.OpenFile(p, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
