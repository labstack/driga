//go:build !windows

package kill

import "syscall"

var signalProcess = syscall.Kill
