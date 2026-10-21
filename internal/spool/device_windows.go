package spool

import (
	"errors"
	"golang.org/x/sys/windows"
	"syscall"
)

func crossDevice(err error) bool {
	return errors.Is(err, syscall.EXDEV) || errors.Is(err, windows.ERROR_NOT_SAME_DEVICE)
}
