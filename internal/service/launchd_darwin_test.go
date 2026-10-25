package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	ks "github.com/kardianos/service"
	"github.com/labstack/driga/internal/config"
)

type fakeLaunchd struct {
	Controller
	stops  int
	starts int
	state  ks.Status
}

func (f *fakeLaunchd) Status() (ks.Status, error) { return f.state, nil }
func (f *fakeLaunchd) Stop() error                { f.stops++; f.state = ks.StatusStopped; return nil }
func (f *fakeLaunchd) Start() error               { f.starts++; f.state = ks.StatusRunning; return nil }

func TestLaunchdInstallRepairsAndIsIdempotent(t *testing.T) {
	// A plain fixture path, never Library/LaunchAgents. No launchctl or real
	// registration is reachable through this fake controller.
	root := t.TempDir()
	bin := filepath.Join(root, "Driga.app", "Contents", "MacOS", "driga")
	path := filepath.Join(root, "collector.plist")
	old, err := renderLaunchd(configForOS("/opt/homebrew/bin/driga", "darwin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, old, 0o600); err != nil {
		t.Fatal(err)
	}
	controller := &fakeLaunchd{state: ks.StatusRunning}
	registrations := 0
	s := &launchdController{Controller: controller, binPath: bin, plistPath: path,
		register: func(app string) error {
			if app != filepath.Join(root, "Driga.app") {
				t.Fatal(app)
			}
			registrations++
			return nil
		},
	}
	if err := s.Install(); err != nil {
		t.Fatal(err)
	}
	if controller.stops != 1 {
		t.Fatalf("stops = %d", controller.stops)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	if controller.starts != 1 {
		t.Fatal("Start wasn't idempotent")
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := PlistStale(path, bin)
	if err != nil || stale {
		t.Fatalf("repair: %t %v", stale, err)
	}
	if err := s.Install(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) || controller.stops != 1 || registrations != 2 {
		t.Fatal("reinstallation changed a current service")
	}
	// Missing association is repairable even when ProgramArguments is current.
	withoutAssociation := strings.Replace(string(after), "<key>AssociatedBundleIdentifiers</key><array><string>run.driga</string></array>", "", 1)
	if err := os.WriteFile(path, []byte(withoutAssociation), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Install(); err != nil {
		t.Fatal(err)
	}
	if controller.stops != 2 {
		t.Fatal("association wasn't repaired")
	}
}

func TestLaunchdDataDir(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "collector.plist")
	s := &launchdController{Controller: &fakeLaunchd{}, binPath: "/usr/local/bin/driga", plistPath: path}
	// A service installed without DRIGA_DATA_DIR collects where an app
	// started without it does.
	t.Setenv("DRIGA_DATA_DIR", "")
	for _, tc := range []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"PATH": "/usr/bin"}, config.DataDir()},
		{map[string]string{"DRIGA_DATA_DIR": filepath.Join(root, "data")}, filepath.Join(root, "data")},
	} {
		c := configForOS(s.binPath, "darwin")
		c.EnvVars = tc.env
		data, err := renderLaunchd(c)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if got, err := DataDir(s); err != nil || got != tc.want {
			t.Fatalf("DataDir with %v = %q, %v; want %q", tc.env, got, err, tc.want)
		}
	}
}
