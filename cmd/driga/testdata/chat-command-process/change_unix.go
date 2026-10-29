//go:build !windows

package main

import (
	"os"
	"syscall"
)

func change() error {
	return syscall.Exec(os.Args[0], []string{os.Args[0], "changed-identity"}, os.Environ())
}
