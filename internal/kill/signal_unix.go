//go:build !windows

package kill

import (
	"github.com/labstack/driga/internal/model"
	"syscall"
)

var signalProcess = syscall.Kill

func signalIdentity(p model.Proc, sig syscall.Signal) error { return signalProcess(int(p.PID), sig) }
