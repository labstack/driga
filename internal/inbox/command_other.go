//go:build !windows

package inbox

import "os/exec"

func configureCommand(*exec.Cmd) {}
