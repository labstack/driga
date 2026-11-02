package service

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	ks "github.com/kardianos/service"
	"github.com/labstack/driga/internal/config"
)

// fakeLaunchctl models one launchd domain the way launchctl reports it: no
// real launchctl is reachable. bootstrap reads the plist at that moment, as
// launchd does, and refuses a job the domain already has.
type fakeLaunchctl struct {
	t                *testing.T
	target, domain   string
	loaded, running  bool
	exists           func() bool // whether the job's executable exists
	definition       string      // the plist launchd bootstrapped
	calls            []string
	bootouts, starts int
}

func (f *fakeLaunchctl) run(args ...string) (string, int, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	switch {
	case slices.Equal(args, []string{"print", f.target}):
		if !f.loaded {
			return "Bad request.\nCould not find service in domain", 113, nil
		}
		state := "not running"
		if f.running {
			state = "running"
		}
		return f.target + " = {\n\tactive count = 1\n\tstate = " + state + "\n\n\tevent triggers = {\n\t\tstate = running\n\t}\n}\n", 0, nil
	case slices.Equal(args, []string{"bootout", f.target}):
		if !f.loaded {
			return "Boot-out failed: 3: No such process", 3, nil
		}
		f.bootouts++
		f.loaded, f.running = false, false
		return "", 0, nil
	case len(args) == 3 && args[0] == "bootstrap" && args[1] == f.domain:
		if f.loaded {
			return "Bootstrap failed: 5: Input/output error", 5, nil
		}
		data, err := os.ReadFile(args[2])
		if err != nil {
			return "Bootstrap failed: 2: No such file or directory", 2, nil
		}
		f.loaded, f.definition = true, string(data)
		f.spawn()
		return "", 0, nil
	case slices.Equal(args, []string{"kickstart", f.target}), slices.Equal(args, []string{"kickstart", "-k", f.target}):
		if !f.loaded {
			return "Could not find service", 113, nil
		}
		if !f.spawn() {
			return "Could not kickstart service: 2: No such file or directory", 2, nil
		}
		return "", 0, nil
	}
	f.t.Fatalf("unexpected launchctl %q", args)
	return "", 0, errors.New("unreachable")
}

func (f *fakeLaunchctl) spawn() bool {
	if f.exists != nil && !f.exists() {
		f.running = false
		return false
	}
	f.starts++
	f.running = true
	return true
}

// A plain fixture path, never Library/LaunchAgents, and a fake launchctl and
// registration, so no real launch agent is reachable.
func newTestLaunchd(t *testing.T) (*launchdController, *fakeLaunchctl, string) {
	t.Helper()
	root := t.TempDir()
	bin := filepath.Join(root, "Driga.app", "Contents", "MacOS", "driga")
	fake := &fakeLaunchctl{t: t, domain: "gui/501", target: "gui/501/" + Label("darwin")}
	s := &launchdController{binPath: bin, label: Label("darwin"), domain: fake.domain,
		plistPath: filepath.Join(root, "collector.plist"), launchctl: fake.run,
		register: func(app string) error {
			if app != filepath.Join(root, "Driga.app") && app != filepath.Join(root, "Driga.app", "Contents", "Helpers", "Driga Service.app") {
				t.Fatal(app)
			}
			return nil
		},
	}
	return s, fake, bin
}

func TestLaunchdInstallRepairsAndIsIdempotent(t *testing.T) {
	s, fake, bin := newTestLaunchd(t)
	old, err := renderLaunchd(configForOS(bin, "darwin"))
	if err != nil {
		t.Fatal(err)
	}
	old = []byte(strings.ReplaceAll(strings.ReplaceAll(string(old), "Contents/Helpers/Driga Service.app/Contents/MacOS/drigad", "Contents/MacOS/drigad"), "<string>run.driga.service</string>", "<string>run.driga</string>"))
	if err := os.WriteFile(s.plistPath, old, 0o600); err != nil {
		t.Fatal(err)
	}
	fake.loaded, fake.running, fake.definition = true, true, string(old)
	if err := s.Install(); err != nil {
		t.Fatal(err)
	}
	if fake.bootouts != 1 || fake.loaded {
		t.Fatalf("bootouts = %d, loaded = %t", fake.bootouts, fake.loaded)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	if fake.starts != 1 {
		t.Fatal("Start wasn't idempotent")
	}
	before, err := os.ReadFile(s.plistPath)
	if err != nil {
		t.Fatal(err)
	}
	if fake.definition != string(before) {
		t.Fatal("launchd runs another definition than the plist")
	}
	stale, err := PlistStale(s.plistPath, bin)
	if err != nil || stale {
		t.Fatalf("repair: %t %v", stale, err)
	}
	if err := s.Install(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(s.plistPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) || fake.bootouts != 1 {
		t.Fatal("reinstallation changed a current service")
	}
	// Missing association is repairable even when ProgramArguments is current.
	withoutAssociation := strings.Replace(string(after), "<key>AssociatedBundleIdentifiers</key><array><string>run.driga.service</string></array>", "", 1)
	if err := os.WriteFile(s.plistPath, []byte(withoutAssociation), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Install(); err != nil {
		t.Fatal(err)
	}
	if fake.bootouts != 2 {
		t.Fatal("association wasn't repaired")
	}
}

// After Driga.app moves, launchd still has the job but KeepAlive cannot run
// its vanished executable: loaded, not running. Repair must boot it out and
// bootstrap the new plist; the deprecated load answered "already loaded".
func TestLaunchdRepairsLoadedJobThatIsNotRunning(t *testing.T) {
	s, fake, bin := newTestLaunchd(t)
	moved := strings.Replace(bin, "Driga.app", "Old/Driga.app", 1)
	old, err := renderLaunchd(configForOS(moved, "darwin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.plistPath, old, 0o600); err != nil {
		t.Fatal(err)
	}
	newDaemon := DaemonPath(ExecutablePath(bin))
	fake.loaded, fake.definition = true, string(old)
	fake.exists = func() bool { return strings.Contains(fake.definition, "<string>"+newDaemon+"</string>") }
	if status, err := s.Status(); err != nil || status != ks.StatusStopped {
		t.Fatalf("Status = %v, %v; want stopped", status, err)
	}
	if err := s.Install(); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	if !fake.loaded || !fake.running || !strings.Contains(fake.definition, newDaemon) {
		t.Fatalf("loaded=%t running=%t; launchd definition:\n%s", fake.loaded, fake.running, fake.definition)
	}
	for _, call := range fake.calls {
		if strings.HasPrefix(call, "load") || strings.HasPrefix(call, "unload") {
			t.Fatalf("deprecated launchctl %s", call)
		}
	}
	if status, err := s.Status(); err != nil || status != ks.StatusRunning {
		t.Fatalf("Status = %v, %v; want running", status, err)
	}
}

// A loaded job whose process exited is kicked, not bootstrapped again.
func TestLaunchdStartKicksLoadedJob(t *testing.T) {
	s, fake, _ := newTestLaunchd(t)
	if err := s.Install(); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	fake.running = false
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	if !fake.running || fake.calls[len(fake.calls)-1] != "kickstart "+fake.target {
		t.Fatalf("calls: %q", fake.calls)
	}
	if err := s.Restart(); err != nil || fake.calls[len(fake.calls)-1] != "kickstart -k "+fake.target {
		t.Fatalf("Restart: %v, calls %q", err, fake.calls)
	}
}

func TestLaunchdStopAndUninstall(t *testing.T) {
	s, fake, _ := newTestLaunchd(t)
	if err := s.Uninstall(); !errors.Is(err, ks.ErrNotInstalled) {
		t.Fatalf("Uninstall of nothing = %v", err)
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop of an unloaded job: %v", err)
	}
	if err := s.Install(); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	if err := s.Stop(); err != nil || fake.loaded {
		t.Fatalf("Stop: %v, loaded=%t", err, fake.loaded)
	}
	if status, err := s.Status(); err != nil || status != ks.StatusStopped {
		t.Fatalf("Status = %v, %v; want stopped", status, err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	if err := s.Uninstall(); err != nil || fake.loaded {
		t.Fatalf("Uninstall: %v, loaded=%t", err, fake.loaded)
	}
	if _, err := os.Stat(s.plistPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("plist left behind")
	}
	if _, err := s.Status(); !errors.Is(err, ks.ErrNotInstalled) {
		t.Fatalf("Status after uninstall: %v", err)
	}
}

// The login environment the service records is part of its definition: a
// changed PATH rewrites the plist and replaces the running job.
func TestLaunchdInstallRefreshesEnvironment(t *testing.T) {
	s, fake, _ := newTestLaunchd(t)
	t.Setenv("PATH", "/usr/bin:/bin")
	if err := s.Install(); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "/opt/homebrew/bin:/usr/bin:/bin")
	if err := s.Install(); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	if fake.bootouts != 1 || !strings.Contains(fake.definition, "/opt/homebrew/bin:/usr/bin:/bin") {
		t.Fatalf("bootouts = %d; definition:\n%s", fake.bootouts, fake.definition)
	}
	env, err := RecordedEnvironment(s)
	if err != nil || env["PATH"] != "/opt/homebrew/bin:/usr/bin:/bin" {
		t.Fatalf("RecordedEnvironment = %v, %v", env, err)
	}
}

// The plist keeps the job alive only while its executable exists, so a
// deleted Driga.app does not leave launchd respawning a missing helper.
func TestLaunchdPlistKeepsAliveWhileExecutableExists(t *testing.T) {
	_, _, bin := newTestLaunchd(t)
	data, err := renderLaunchd(configForOS(bin, "darwin"))
	if err != nil {
		t.Fatal(err)
	}
	daemon := DaemonPath(ExecutablePath(bin))
	for _, want := range []string{
		"<key>KeepAlive</key><dict><key>PathState</key><dict><key>" + daemon + "</key><true/></dict></dict>",
		"<key>ProcessType</key><string>Background</string>",
		"<key>ThrottleInterval</key><integer>30</integer>",
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("plist lacks %s:\n%s", want, data)
		}
	}
}

func TestLaunchdDataDir(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "collector.plist")
	s := &launchdController{binPath: "/usr/local/bin/driga", plistPath: path}
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
