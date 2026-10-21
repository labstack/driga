package service

import (
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/labstack/driga/internal/pathx"
)

// taskXML is platform independent so the scheduler contract is tested everywhere.
// InteractiveToken never stores a password and cannot run elevated or at logout.
func taskXML(bin, sid, dataDir string) ([]byte, error) {
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
