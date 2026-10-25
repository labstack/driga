package service

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	ks "github.com/kardianos/service"
)

func TestServiceConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix service or hook path contract; Windows has watch/refusal tests")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	c := configForOS("/opt/driga", "darwin")
	if c.Name != "run.driga.service" || c.DisplayName != "Driga Service" || c.Executable != "/opt/drigad" || !reflect.DeepEqual(c.Arguments, []string{"--owner=service"}) {
		t.Fatalf("unexpected config: %+v", c)
	}
	for _, key := range []string{"UserService", "KeepAlive", "RunAtLoad"} {
		if c.Option[key] != true {
			t.Errorf("%s = %v", key, c.Option[key])
		}
	}
	// Only inspect configuration: never construct/control a real service here.
	template, ok := c.Option["LaunchdConfig"].(string)
	if !ok || strings.Count(template, filepath.Join(home, "Library", "Logs", "driga", "service.log")) != 2 {
		t.Fatalf("launchd must send stdout and stderr to service.log: %s", template)
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
	if c.Name != "driga" || !reflect.DeepEqual(c.Arguments, []string{"--owner=service"}) {
		t.Fatal(c.Arguments)
	}
	if c.Option["UserService"] != true || c.Option["Restart"] != "always" || c.Option["LogOutput"] != false || c.EnvVars["XDG_DATA_HOME"] != filepath.Join(home, "xdg-data") {
		t.Fatalf("%+v", c)
	}
	if _, ok := c.Option["LaunchdConfig"]; ok {
		t.Fatal("launchd options on Linux")
	}
	template, _ := c.Option["SystemdScript"].(string)
	if c.DisplayName != "Driga Service" || !strings.Contains(template, "Description=Driga service\n") || !strings.Contains(template, "WantedBy=default.target") || !strings.Contains(template, "StandardOutput=journal") {
		t.Fatal(template)
	}
	if hint := InstallHint("linux"); !strings.Contains(hint, "loginctl enable-linger $USER") {
		t.Fatal(hint)
	}
	if hint := InstallHint("darwin"); hint != "" {
		t.Fatal(hint)
	}
	if got := logPathForOS("linux"); got != filepath.Join(home, "xdg-state/driga/service.log") {
		t.Fatal(got)
	}
	t.Setenv("XDG_STATE_HOME", "")
	if got := logPathForOS("linux"); got != filepath.Join(home, ".local/state/driga/service.log") {
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

type statusController struct {
	Controller
	status ks.Status
	err    error
}

func (c statusController) Status() (ks.Status, error) { return c.status, c.err }

func TestInstalled(t *testing.T) {
	failed := errors.New("launchctl failed")
	for _, tc := range []struct {
		c       statusController
		want    bool
		wantErr error
	}{
		{statusController{status: ks.StatusRunning}, true, nil},
		{statusController{status: ks.StatusStopped}, true, nil},
		{statusController{status: ks.StatusUnknown, err: ks.ErrNotInstalled}, false, nil},
		{statusController{status: ks.StatusUnknown, err: failed}, false, failed},
	} {
		got, err := Installed(tc.c)
		if got != tc.want || !errors.Is(err, tc.wantErr) {
			t.Errorf("Installed(%v, %v) = %t, %v; want %t, %v", tc.c.status, tc.c.err, got, err, tc.want, tc.wantErr)
		}
	}
}

// drigad sits beside driga under its variant's name, and each variant has
// its own label, so a development copy never takes over the release's.
func TestDaemonPathAndVariant(t *testing.T) {
	for in, want := range map[string]string{
		"/opt/driga": "/opt/drigad", "/opt/driga-dev": "/opt/drigad-dev", "/opt/driga.exe": "/opt/drigad.exe", "/opt/drigad": "/opt/drigad", "/opt/other": "/opt/other",
	} {
		if got := DaemonPath(in); got != want {
			t.Errorf("DaemonPath(%q) = %q, want %q", in, got, want)
		}
	}
	for exe, want := range map[string]string{"/opt/driga": "", "/opt/drigad": "", "/opt/driga-dev": "-dev", "/opt/drigad-dev": "-dev", "/opt/driga.exe": "", "/opt/service.test": ""} {
		if got := variantOf(exe); got != want {
			t.Errorf("variantOf(%q) = %q, want %q", exe, got, want)
		}
	}
	if !IsDaemon("/opt/drigad-dev") || !IsDaemon("drigad.exe") || IsDaemon("/opt/driga") || IsDaemon("/opt/driga-dev") {
		t.Error("IsDaemon")
	}
}
