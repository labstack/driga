package cli

import (
	"bytes"
	"os"
	"testing"

	"github.com/labstack/driga/internal/desktop"
)

func TestAppLaunch(t *testing.T) {
	env := func(value string) func(string) string {
		return func(name string) string {
			if name == "DRIGA_APP" {
				return value
			}
			return ""
		}
	}
	for _, tt := range []struct {
		name  string
		env   string
		login bool
		args  []string
		want  bool
	}{
		{"bare from Finder", "1", false, nil, true},
		{"bare at login", "", true, nil, true},
		{"bare in a terminal", "", false, nil, false},
		{"command", "1", false, []string{"top"}, false},
		{"command at login", "", true, []string{"top"}, false},
		{"app", "", false, []string{"app"}, true},
		{"app help", "", false, []string{"app", "--help"}, false},
		{"other value", "0", false, nil, false},
	} {
		if got := appLaunch(env(tt.env), tt.login, tt.args); got != tt.want {
			t.Errorf("%s: appLaunch(%q, %v, %q) = %v, want %v", tt.name, tt.env, tt.login, tt.args, got, tt.want)
		}
	}
}

// The app and the processes it starts must never see DRIGA_APP: config
// loading rejects unknown DRIGA_* names.
func TestAppLaunchUnsetsMarker(t *testing.T) {
	login := false
	defer func(prev func() bool) { openedAtLogin, systemLaunch = prev, false }(openedAtLogin)
	openedAtLogin = func() bool { return login }

	t.Setenv("DRIGA_APP", "1")
	if !AppLaunch(nil) || !systemLaunch {
		t.Fatal("DRIGA_APP=1 did not start the app")
	}
	if _, ok := os.LookupEnv("DRIGA_APP"); ok {
		t.Fatal("DRIGA_APP still set")
	}

	t.Setenv("DRIGA_APP", "1")
	if AppLaunch([]string{"top"}) || systemLaunch {
		t.Fatal("a command started the app")
	}
	if _, ok := os.LookupEnv("DRIGA_APP"); ok {
		t.Fatal("DRIGA_APP still set")
	}

	login = true
	if !AppLaunch(nil) || !systemLaunch {
		t.Fatal("a login launch did not start the app")
	}
	login = false
	if !AppLaunch([]string{"app"}) || systemLaunch {
		t.Fatal("driga app counted as a system launch")
	}
}

func TestAppCommandRunsDesktop(t *testing.T) {
	readEnv(t)
	defer func(prev func(desktop.Options) error) { runDesktop = prev }(runDesktop)
	var got desktop.Options
	var env string
	runDesktop = func(opts desktop.Options) error {
		got, env = opts, os.Getenv("DRIGA_APP")
		return nil
	}
	defer func(prev bool) { systemLaunch = prev }(systemLaunch)
	systemLaunch = true
	t.Setenv("DRIGA_APP", "1")
	if !AppLaunch(nil) {
		t.Fatal("not app mode")
	}
	var out, errout bytes.Buffer
	if code := run([]string{"app"}, "test-version", nil, &out, &errout); code != 0 {
		t.Fatalf("driga app: %d %s", code, errout.String())
	}
	if env != "" {
		t.Fatalf("app started with DRIGA_APP=%q", env)
	}
	if got.Source == nil || got.Supervisor == nil || got.Version != "test-version" {
		t.Fatalf("options: %+v", got)
	}
	if got.Web.SessionAction == nil || got.Web.CleanupList == nil || got.Web.Activity == nil || got.Web.Machine != "all" {
		t.Fatalf("page handlers: %+v", got.Web)
	}
}
