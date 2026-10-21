package inbox

import (
	"os/exec"
	"strings"
	"syscall"
)

func configureCommand(cmd *exec.Cmd) {
	// cmd.exe does not use CommandLineToArgvW. Build its exact START line with
	// an explicitly quoted directory, including when it contains no spaces.
	if strings.EqualFold(cmd.Args[0], "cmd.exe") && len(cmd.Args) == 7 && cmd.Args[1] == "/c" && cmd.Args[2] == "start" && cmd.Args[3] == "" && cmd.Args[4] == "/D" && cmd.Args[6] == "cmd.exe" {
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: windowsTerminalLine(cmd.Args[5]), HideWindow: true}
	}
}
