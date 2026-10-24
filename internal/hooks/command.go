package hooks

import (
	"fmt"
	"strings"

	"github.com/labstack/driga/internal/pathx"
)

func hookEntry(bin, adapter, event, goos string) (entry map[string]any, err error) {
	defer func() {
		if err == nil && adapter == "claude" && event == "PermissionRequest" {
			entry["timeout"] = 660
		}
	}()
	if goos == "windows" {
		if !pathx.RecordedIsAbs(bin) || !pathx.WindowsStyle(bin) || strings.ContainsAny(bin, "\x00\r\n\"") {
			return nil, fmt.Errorf("hook requires an absolute Windows executable path")
		}
		// Claude's documented exec form avoids Git Bash, cmd, and PowerShell quoting
		// entirely. Quotes here would become literal filename characters.
		return map[string]any{"type": "command", "command": bin, "args": []string{"hook", adapter, event}}, nil
	}
	command, err := hookCommand(bin, adapter, event, goos)
	return map[string]any{"type": "command", "command": command}, err
}

func hookCommand(bin, adapter, event, goos string) (string, error) {
	if goos != "windows" {
		return shellQuote(bin) + " hook " + adapter + " " + event, nil
	}
	// Codex's string hook needs shell form. CALL works from cmd and PowerShell;
	// reject expansion/reparse characters rather than silently installing an
	// unsafe command. Spaces, Unicode, backslashes and apostrophes are supported.
	if !pathx.RecordedIsAbs(bin) || !pathx.WindowsStyle(bin) || strings.ContainsAny(bin, "\x00\r\n\"%!^&|<>()$`") {
		return "", fmt.Errorf("Windows hook path contains shell syntax; install driga.exe at a path without shell metacharacters")
	}
	return `cmd.exe /d /c call "` + bin + `" hook ` + adapter + " " + event, nil
}

// hookProgram also understands the conservative Windows string form. It only
// parses our exact prefix; arbitrary shell wrappers never count as owned hooks.
func hookProgram(command string) (program, rest string, ok bool) {
	if body, found := strings.CutPrefix(command, `cmd.exe /d /c call "`); found {
		program, rest, ok = strings.Cut(body, `"`)
		return program, rest, ok && !strings.ContainsAny(program, "\x00\r\n\"%!^&|<>()$`")
	}
	return shellProgram(command)
}

// Match the complete invocation, never a shell wrapper or trailing command.
func hookArguments(rest, adapter string) bool {
	args := strings.Fields(rest)
	return len(args) == 3 && args[0] == "hook" && args[1] == adapter && validHookEvent(args[2])
}
func validHookEvent(event string) bool {
	if event == "" {
		return false
	}
	for _, r := range event {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}
