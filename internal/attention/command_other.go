//go:build !windows

package attention

import "os/exec"

func configureCommand(*exec.Cmd) {}
