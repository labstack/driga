package service

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"unicode/utf16"
)

// schtasks /XML reads UTF-16 only: a UTF-8 declaration fails with "unable to
// switch the encoding". taskXML stays UTF-8 so the contract tests can parse it.
func taskFile(b []byte) []byte {
	b = bytes.Replace(b, []byte(`encoding="UTF-8"`), []byte(`encoding="UTF-16"`), 1)
	units := utf16.Encode([]rune(string(b)))
	out := make([]byte, 2, 2+2*len(units))
	out[0], out[1] = 0xff, 0xfe // little-endian byte order mark
	for _, u := range units {
		out = append(out, byte(u), byte(u>>8))
	}
	return out
}

// taskArguments reads the collector command's arguments from a task
// definition, as schtasks /Query /XML prints it: UTF-16 with a byte order mark,
// or UTF-8.
func taskArguments(b []byte) (string, error) {
	if len(b) >= 2 && b[0] == 0xff && b[1] == 0xfe {
		units := make([]uint16, (len(b)-2)/2)
		for i := range units {
			units[i] = uint16(b[2+2*i]) | uint16(b[3+2*i])<<8
		}
		b = []byte(string(utf16.Decode(units)))
	}
	var task struct {
		Actions struct {
			Exec []struct{ Arguments string }
		}
	}
	decoder := xml.NewDecoder(bytes.NewReader(b))
	// The text is UTF-8 by now, whatever the declaration says.
	decoder.CharsetReader = func(_ string, input io.Reader) (io.Reader, error) { return input, nil }
	if err := decoder.Decode(&task); err != nil {
		return "", err
	}
	if len(task.Actions.Exec) != 1 {
		return "", errors.New("collector task has no single command")
	}
	return task.Actions.Exec[0].Arguments, nil
}

func taskArgs(action, name, xmlPath string) ([]string, error) {
	switch action {
	case "install":
		return []string{"/Create", "/TN", name, "/XML", xmlPath}, nil
	case "uninstall":
		return []string{"/Delete", "/TN", name, "/F"}, nil
	case "start":
		return []string{"/Run", "/TN", name}, nil
	case "stop":
		return []string{"/End", "/TN", name}, nil
	case "status":
		return []string{"/Query", "/TN", name, "/FO", "CSV", "/NH"}, nil
	case "xml":
		return []string{"/Query", "/TN", name, "/XML"}, nil
	default:
		return nil, fmt.Errorf("unknown task action %q", action)
	}
}

// Only path overrides are persisted in the task; never capture credentials or
// arbitrary process environment. Task Scheduler otherwise uses the login environment.
var scheduledEnvKeys = []string{
	"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "TEMP", "TMP",
	"CLAUDE_CONFIG_DIR", "CODEX_HOME", "COPILOT_HOME", "GEMINI_CLI_HOME",
	"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME",
	"DRIGA_DATA_DIR", "DRIGA_SPOOL_DIR", "DRIGA_CONFIG",
}

func scheduledEnvKey(key string) bool {
	for _, allowed := range scheduledEnvKeys {
		if key == allowed {
			return true
		}
	}
	return false
}
