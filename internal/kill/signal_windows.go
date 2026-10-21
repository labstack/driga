package kill

import (
	"github.com/labstack/driga/internal/platform"
	"syscall"
)

func signalProcess(int, syscall.Signal) error { return platform.ErrWindowsAction }
