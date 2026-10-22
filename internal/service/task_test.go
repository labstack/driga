package service

import (
	"encoding/xml"
	"reflect"
	"strings"
	"testing"
)

func TestTaskXML(t *testing.T) {
	bin := `C:\Tools & More\界\driga.exe`
	b, err := taskXML(bin, "S-1-5-21-123", `C:\data dir`)
	if err != nil {
		t.Fatal(err)
	}
	var task struct {
		XMLName  xml.Name
		Triggers struct {
			LogonTrigger struct {
				UserID string `xml:"UserId"`
			}
		}
		Principals struct {
			Principal struct {
				UserID              string `xml:"UserId"`
				LogonType, RunLevel string
			}
		}
		Settings struct {
			Hidden, DisallowStartIfOnBatteries, StopIfGoingOnBatteries bool
			ExecutionTimeLimit, MultipleInstancesPolicy                string
			RestartOnFailure                                           struct {
				Interval string
				Count    int
			}
		}
		Actions struct {
			Exec struct{ Command, Arguments, WorkingDirectory string }
		}
	}
	if err := xml.Unmarshal(b, &task); err != nil {
		t.Fatal(err)
	}
	if task.XMLName.Space != "http://schemas.microsoft.com/windows/2004/02/mit/task" || task.Triggers.LogonTrigger.UserID != "S-1-5-21-123" || task.Principals.Principal.UserID != "S-1-5-21-123" || task.Principals.Principal.LogonType != "InteractiveToken" || task.Principals.Principal.RunLevel != "LeastPrivilege" {
		t.Fatalf("identity: %+v", task)
	}
	if !task.Settings.Hidden || task.Settings.DisallowStartIfOnBatteries || task.Settings.StopIfGoingOnBatteries || task.Settings.ExecutionTimeLimit != "PT0S" || task.Settings.MultipleInstancesPolicy != "IgnoreNew" || task.Settings.RestartOnFailure.Interval != "PT1M" || task.Settings.RestartOnFailure.Count != 999 {
		t.Fatalf("settings: %+v", task.Settings)
	}
	if task.Actions.Exec.Command != bin || task.Actions.Exec.Arguments != `collect --scheduled --service-data-dir "C:\data dir"` || task.Actions.Exec.WorkingDirectory != `C:\data dir` {
		t.Fatalf("exec: %+v", task.Actions.Exec)
	}
	for _, bad := range []string{"driga.exe", `C:driga.exe`, "C:\\bad\x00.exe"} {
		if _, err := taskXML(bad, "S-1-5-21-123", `C:\data`); err == nil {
			t.Fatal("accepted", bad)
		}
	}
	if strings.Contains(string(b), "PowerShell") {
		t.Fatal("shell wrapper")
	}
	b, err = taskXML(bin, "S-1-5-21-123", `C:\`)
	if err != nil || !strings.Contains(string(b), `C:\\&#34;`) {
		t.Fatal("drive-root quoting", string(b), err)
	}
}

func TestTaskArgs(t *testing.T) {
	for _, tc := range []struct {
		action string
		want   []string
	}{
		{"install", []string{"/Create", "/TN", `\Driga\test`, "/XML", `C:\a b\task.xml`}},
		{"uninstall", []string{"/Delete", "/TN", `\Driga\test`, "/F"}},
		{"start", []string{"/Run", "/TN", `\Driga\test`}},
		{"stop", []string{"/End", "/TN", `\Driga\test`}},
		{"status", []string{"/Query", "/TN", `\Driga\test`, "/FO", "CSV", "/NH"}},
	} {
		got, err := taskArgs(tc.action, `\Driga\test`, `C:\a b\task.xml`)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatal(tc.action, got, err)
		}
	}
	if _, err := taskArgs("bad", `\Driga\test`, ""); err == nil {
		t.Fatal("unknown action")
	}
}

func TestTaskEnvironment(t *testing.T) {
	b, err := taskXML(`C:\Tools\driga.exe`, "S-1-5-21-123", `C:\data`, `USERPROFILE=C:\isolated home\`, `CLAUDE_CONFIG_DIR=`, `DRIGA_SPOOL_DIR=C:\spool & events`)
	if err != nil {
		t.Fatal(err)
	}
	var task struct {
		Actions struct{ Exec struct{ Arguments string } }
	}
	if err := xml.Unmarshal(b, &task); err != nil {
		t.Fatal(err)
	}
	for _, arg := range []string{`--service-env "USERPROFILE=C:\isolated home\\"`, `--service-env "CLAUDE_CONFIG_DIR="`, `--service-env "DRIGA_SPOOL_DIR=C:\spool & events"`} {
		if !strings.Contains(task.Actions.Exec.Arguments, arg) {
			t.Fatalf("missing %q: %s", arg, task.Actions.Exec.Arguments)
		}
	}
	for _, bad := range []string{"TOKEN=secret", "PATH=C:\\tools", "HOME", "HOME=C:\\bad\npath", `HOME=C:\bad"path`} {
		if _, err := taskXML(`C:\Tools\driga.exe`, "S-1-5-21-123", `C:\data`, bad); err == nil {
			t.Fatalf("accepted non-path/invalid environment %q", bad)
		}
	}
}
