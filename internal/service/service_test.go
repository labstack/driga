package service

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestServiceConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	c := configForOS("/opt/driga", "darwin")
	if c.Name != "run.driga.collector" || c.DisplayName != "driga collector" || c.Executable != "/opt/driga" || !reflect.DeepEqual(c.Arguments, []string{"collect"}) {
		t.Fatalf("unexpected config: %+v", c)
	}
	for _, key := range []string{"UserService", "KeepAlive", "RunAtLoad"} {
		if c.Option[key] != true {
			t.Errorf("%s = %v", key, c.Option[key])
		}
	}
	// Only inspect configuration: never construct/control a real service here.
	template, ok := c.Option["LaunchdConfig"].(string)
	if !ok || strings.Count(template, filepath.Join(home, "Library", "Logs", "driga", "collector.log")) != 2 {
		t.Fatalf("launchd must send stdout and stderr to collector.log: %s", template)
	}
}

func TestServiceConfigLinux(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "xdg-data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "xdg-state"))
	c := configForOS("/opt/driga", "linux")
	if c.Option["UserService"] != true || c.Option["Restart"] != "always" || c.Option["LogOutput"] != false || c.EnvVars["XDG_DATA_HOME"] != filepath.Join(home, "xdg-data") {
		t.Fatalf("%+v", c)
	}
	if _, ok := c.Option["LaunchdConfig"]; ok {
		t.Fatal("launchd options on Linux")
	}
	template, _ := c.Option["SystemdScript"].(string)
	if !strings.Contains(template, "WantedBy=default.target") || !strings.Contains(template, "StandardOutput=journal") {
		t.Fatal(template)
	}
	if hint := InstallHint("linux"); !strings.Contains(hint, "loginctl enable-linger $USER") {
		t.Fatal(hint)
	}
	if hint := InstallHint("darwin"); hint != "" {
		t.Fatal(hint)
	}
	if got := logPathForOS("linux"); got != filepath.Join(home, "xdg-state/driga/collector.log") {
		t.Fatal(got)
	}
	t.Setenv("XDG_STATE_HOME", "")
	if got := logPathForOS("linux"); got != filepath.Join(home, ".local/state/driga/collector.log") {
		t.Fatal(got)
	}
}
