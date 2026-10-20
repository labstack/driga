package platform

import "os/exec"

// Windows has no Unix process groups; native tools are disabled for Chat.
func OwnChild(cmd *exec.Cmd) func() { return func() {} }
