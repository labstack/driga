//go:build !windows

package spool

import (
	"errors"
	"syscall"
)

func crossDevice(err error) bool { return errors.Is(err, syscall.EXDEV) }
