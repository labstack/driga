package service

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestServiceConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix service or hook path contract; Windows has watch/refusal tests")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
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
	if runtime.GOOS == "windows" {
		t.Skip("Unix service or hook path contract; Windows has watch/refusal tests")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
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

func TestServiceCarriesConfigurationEnvironment(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("DRIGA_CAPTURE_TEXT", "snippets")
	t.Setenv("DRIGA_SUMMARIES_ENABLED", "true")
	t.Setenv("DRIGA_INTERNAL", "1")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	for _, goos := range []string{"darwin", "linux"} {
		c := configForOS("/opt/driga", goos)
		for _, name := range []string{"DRIGA_CAPTURE_TEXT", "DRIGA_SUMMARIES_ENABLED", "XDG_CONFIG_HOME"} {
			if c.EnvVars[name] == "" {
				t.Fatal("missing service env", name)
			}
		}
		if c.EnvVars["DRIGA_INTERNAL"] != "" {
			t.Fatal("internal test marker propagated")
		}
	}
}

// The collector runs tmux and agent CLIs (tmux list-panes, claude agents
// --json). launchd and systemd start services with a bare PATH, so the
// service must carry the installing shell's PATH or those lookups fail
// silently and every jump falls back to a resume command.
func TestServiceCarriesPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("PATH", "/opt/tools/bin:/usr/bin:/bin")
	t.Setenv("TMUX_TMPDIR", "/run/user/tmux")
	for _, goos := range []string{"darwin", "linux"} {
		c := configForOS("/opt/tools/bin/driga", goos)
		if c.EnvVars["PATH"] != "/opt/tools/bin:/usr/bin:/bin" || c.EnvVars["TMUX_TMPDIR"] != "/run/user/tmux" {
			t.Fatalf("%s env = %v", goos, c.EnvVars)
		}
	}
}
