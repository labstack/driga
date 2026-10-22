package service

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/labstack/driga/internal/pathx"
)

// taskXML is platform independent so the scheduler contract is tested everywhere.
// InteractiveToken never stores a password and cannot run elevated or at logout.
func taskXML(bin, sid, dataDir string, env ...string) ([]byte, error) {
	for _, p := range []string{bin, dataDir} {
		if !pathx.WindowsStyle(p) || !pathx.RecordedIsAbs(p) || strings.ContainsAny(p, "\x00\r\n\"") {
			return nil, fmt.Errorf("task path must be an absolute Windows path")
		}
	}
	if !strings.HasPrefix(sid, "S-1-") || strings.ContainsAny(sid, "<>&\"\x00\r\n") {
		return nil, fmt.Errorf("task requires the current user's SID")
	}
	escape := func(s string) string { var b strings.Builder; _ = xml.EscapeText(&b, []byte(s)); return b.String() }
	// None of these values are interpreted by a shell. Double trailing
	// backslashes before the closing quote for CommandLineToArgvW, including a
	// drive root such as C:\. Embedded quotes have already been refused above.
	trailing := len(dataDir) - len(strings.TrimRight(dataDir, `\`))
	args := `collect --scheduled --service-data-dir "` + dataDir + strings.Repeat(`\`, trailing) + `"`
	for _, value := range env {
		key, _, ok := strings.Cut(value, "=")
		if !ok || !scheduledEnvKey(key) || strings.ContainsAny(value, "\x00\r\n\"") {
			return nil, fmt.Errorf("invalid scheduled collector environment")
		}
		trailing := len(value) - len(strings.TrimRight(value, `\`))
		args += ` --service-env "` + value + strings.Repeat(`\`, trailing) + `"`
	}
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
 <RegistrationInfo><Description>Driga per-user collector</Description></RegistrationInfo>
 <Triggers><LogonTrigger><Enabled>true</Enabled><UserId>` + escape(sid) + `</UserId></LogonTrigger></Triggers>
 <Principals><Principal id="User"><UserId>` + escape(sid) + `</UserId><LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals>
 <Settings>
  <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
  <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries><StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
  <AllowHardTerminate>true</AllowHardTerminate><StartWhenAvailable>true</StartWhenAvailable>
  <AllowStartOnDemand>true</AllowStartOnDemand><Enabled>true</Enabled><Hidden>true</Hidden>
  <ExecutionTimeLimit>PT0S</ExecutionTimeLimit><RestartOnFailure><Interval>PT1M</Interval><Count>999</Count></RestartOnFailure>
 </Settings>
 <Actions Context="User"><Exec><Command>` + escape(bin) + `</Command><Arguments>` + escape(args) + `</Arguments><WorkingDirectory>` + escape(dataDir) + `</WorkingDirectory></Exec></Actions>
</Task>
`), nil
}

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
