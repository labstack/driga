package web

import (
	"os/exec"
	"strings"
)

// nodeScript feeds a test script on stdin: Windows limits a command line to
// 32,767 characters, which the bundled page scripts exceed with -e.
func nodeScript(runtime, script string) *exec.Cmd {
	cmd := exec.Command(runtime, "-")
	cmd.Stdin = strings.NewReader(script)
	return cmd
}
