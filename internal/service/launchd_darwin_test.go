package service

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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
	// noDomain models an account without a desktop session: gui/<uid>
	// does not exist.
	noDomain bool
	// linger keeps a booted-out job loaded for that many print calls, as
	// launchd finishes tearing it down after bootout returns; bootstrap
	// and kickstart meanwhile fail.
	linger, tearingDown int
	// refuse makes bootstrap fail with code 78 for a definition it matches.
	refuse func(definition string) bool
}

func (f *fakeLaunchctl) run(args ...string) (string, int, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	if f.noDomain {
		return "Bad request.\nCould not find domain for user gui: 501", 112, nil
	}
	switch {
	case slices.Equal(args, []string{"print", f.target}):
		if f.tearingDown > 0 {
			f.tearingDown--
			if f.tearingDown == 0 {
				f.loaded = false
			}
			return f.target + " = {\n\tstate = not running\n}\n", 0, nil
		}
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
		f.running = false
		if f.linger > 0 {
			f.tearingDown = f.linger
		} else {
			f.loaded = false
		}
		return "", 0, nil
	case len(args) == 3 && args[0] == "bootstrap" && args[1] == f.domain:
		if f.tearingDown > 0 {
			return "Bootstrap failed: 37: Operation already in progress", 37, nil
		}
		if f.loaded {
			return "Bootstrap failed: 5: Input/output error", 5, nil
		}
		data, err := os.ReadFile(args[2])
		if err != nil {
			return "Bootstrap failed: 2: No such file or directory", 2, nil
		}
		if f.refuse != nil && f.refuse(string(data)) {
			return "Bootstrap failed: 78: Function not implemented", 78, nil
		}
		f.loaded, f.definition = true, string(data)
		f.spawn()
		return "", 0, nil
	case slices.Equal(args, []string{"kickstart", f.target}), slices.Equal(args, []string{"kickstart", "-k", f.target}):
		if f.tearingDown > 0 {
			f.t.Fatalf("kickstart of a job being torn down; calls %q", f.calls)
		}
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
		sleep: func(time.Duration) {},
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
	if fake.bootouts != 1 || !fake.loaded || !fake.running {
		t.Fatalf("bootouts = %d, loaded = %t, running = %t", fake.bootouts, fake.loaded, fake.running)
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

// The login environment the service records is part of its definition: with
// --refresh-env, as the app installs, a changed PATH rewrites the plist and
// replaces the running job.
func TestLaunchdInstallRefreshesEnvironment(t *testing.T) {
	s, fake, _ := newTestLaunchd(t)
	s.refreshEnvironment()
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

// launchd finishes tearing a job down after bootout returns. Install waits
// until print no longer has it before it bootstraps the new definition, and
// never kicks the old one.
func TestLaunchdInstallWaitsForTeardown(t *testing.T) {
	s, fake, bin := newTestLaunchd(t)
	old := strings.Replace(mustRender(t, bin), "<key>ThrottleInterval</key>", "<key>Old</key><true/><key>ThrottleInterval</key>", 1)
	if err := os.WriteFile(s.plistPath, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	fake.loaded, fake.running, fake.definition, fake.linger = true, true, old, 3
	if err := s.Install(); err != nil {
		t.Fatal(err)
	}
	if !fake.loaded || !fake.running || fake.definition == old {
		t.Fatalf("loaded=%t running=%t; calls %q", fake.loaded, fake.running, fake.calls)
	}
	bootout, bootstrap := slices.Index(fake.calls, "bootout "+fake.target), slices.IndexFunc(fake.calls, func(c string) bool { return strings.HasPrefix(c, "bootstrap ") })
	if bootout < 0 || bootstrap-bootout != 5 {
		t.Fatalf("bootstrap must follow three teardown prints and the unloaded one: %q", fake.calls)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	// Stop waits too, so Start right after it bootstraps.
	fake.linger = 4
	if err := s.Stop(); err != nil || fake.loaded {
		t.Fatalf("Stop: %v, loaded=%t", err, fake.loaded)
	}
	if err := s.Start(); err != nil || !fake.running {
		t.Fatalf("Start: %v, running=%t", err, fake.running)
	}
}

// A job another client is booting out is not kicked: Start lets it go and
// bootstraps the plist.
func TestLaunchdStartDoesNotKickJobBeingTornDown(t *testing.T) {
	s, fake, bin := newTestLaunchd(t)
	if err := os.WriteFile(s.plistPath, []byte(mustRender(t, bin)), 0o600); err != nil {
		t.Fatal(err)
	}
	fake.loaded, fake.tearingDown = true, 3
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	if !fake.loaded || !fake.running {
		t.Fatalf("calls %q", fake.calls)
	}
}

// bootstrap retries while launchd answers that it is busy with the job, with
// growing backoff.
func TestLaunchdBootstrapRetriesWhileBusy(t *testing.T) {
	s, fake, bin := newTestLaunchd(t)
	if err := os.WriteFile(s.plistPath, []byte(mustRender(t, bin)), 0o600); err != nil {
		t.Fatal(err)
	}
	var sleeps []time.Duration
	s.sleep = func(d time.Duration) { sleeps = append(sleeps, d) }
	fake.loaded, fake.tearingDown = true, 2
	if err := s.bootstrap(); err != nil {
		t.Fatal(err)
	}
	if !fake.running || !slices.Equal(sleeps, []time.Duration{unloadPoll, 2 * unloadPoll}) {
		t.Fatalf("running=%t sleeps=%v calls=%q", fake.running, sleeps, fake.calls)
	}
}

// When launchd never lets the old job go, Install gives up after unloadWait
// and leaves the old plist in place.
func TestLaunchdInstallBoundsTeardownWait(t *testing.T) {
	s, fake, bin := newTestLaunchd(t)
	old := strings.Replace(mustRender(t, bin), "<key>ThrottleInterval</key>", "<key>Old</key><true/><key>ThrottleInterval</key>", 1)
	if err := os.WriteFile(s.plistPath, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	var waited time.Duration
	s.sleep = func(d time.Duration) { waited += d }
	fake.loaded, fake.definition, fake.linger = true, old, 1<<20
	if err := s.Install(); err == nil || !strings.Contains(err.Error(), "after bootout") {
		t.Fatalf("Install = %v", err)
	}
	if waited != unloadWait {
		t.Fatalf("waited %s, want %s", waited, unloadWait)
	}
	if data, _ := os.ReadFile(s.plistPath); string(data) != old {
		t.Fatal("plist replaced although the old job was still loaded")
	}
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(s.plistPath), ".*"))
	if len(matches) != 0 {
		t.Fatalf("temporary plist left behind: %v", matches)
	}
}

// A definition launchd refuses does not cost the working service: the
// previous plist is restored and bootstrapped again.
func TestLaunchdInstallRestoresPreviousDefinition(t *testing.T) {
	s, fake, bin := newTestLaunchd(t)
	old := strings.Replace(mustRender(t, bin), "<key>ThrottleInterval</key>", "<key>Old</key><true/><key>ThrottleInterval</key>", 1)
	if err := os.WriteFile(s.plistPath, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	fake.loaded, fake.running, fake.definition = true, true, old
	fake.refuse = func(definition string) bool { return definition != old }
	err := s.Install()
	if err == nil || !strings.Contains(err.Error(), "previous one runs again") {
		t.Fatalf("Install = %v", err)
	}
	if data, _ := os.ReadFile(s.plistPath); string(data) != old {
		t.Fatal("previous plist not restored")
	}
	if !fake.loaded || !fake.running || fake.definition != old {
		t.Fatalf("previous definition not running again: loaded=%t running=%t", fake.loaded, fake.running)
	}
	// With nothing to restore the error is launchd's.
	if err := os.Remove(s.plistPath); err != nil {
		t.Fatal(err)
	}
	fake.loaded, fake.running = false, false
	if err := s.Install(); err == nil || !strings.Contains(err.Error(), "exited 78") {
		t.Fatalf("fresh Install = %v", err)
	}
}

// Over SSH with nobody logged in to the desktop there is no gui/<uid>
// domain. Uninstall still deletes the plist; start and status say what to
// do; install leaves the plist for launchd to load at the next login.
func TestLaunchdWithoutDesktopSession(t *testing.T) {
	s, fake, bin := newTestLaunchd(t)
	fake.noDomain = true
	if _, err := s.Status(); !errors.Is(err, ks.ErrNotInstalled) {
		t.Fatalf("Status without plist = %v", err)
	}
	if err := s.Install(); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(s.plistPath); err != nil || string(data) != mustRender(t, bin) {
		t.Fatalf("plist not written: %v", err)
	}
	for name, f := range map[string]func() error{"Start": s.Start, "Restart": s.Restart, "Status": func() error { _, err := s.Status(); return err }} {
		if err := f(); !errors.Is(err, ErrNoGUISession) || !strings.Contains(err.Error(), "log in to the desktop session to start the Driga service") {
			t.Errorf("%s = %v", name, err)
		}
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop = %v", err)
	}
	if err := s.Uninstall(); err != nil {
		t.Fatalf("Uninstall = %v", err)
	}
	if _, err := os.Stat(s.plistPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("plist left behind")
	}
	if err := s.Uninstall(); !errors.Is(err, ks.ErrNotInstalled) {
		t.Fatalf("second Uninstall = %v", err)
	}
}

// Without --refresh-env, as from a terminal, an installed service keeps the
// environment it records, so a terminal install does not restart it.
func TestLaunchdInstallKeepsRecordedEnvironment(t *testing.T) {
	s, fake, _ := newTestLaunchd(t)
	t.Setenv("PATH", "/login/bin:/usr/bin")
	s.refreshEnv = true
	if err := s.Install(); err != nil {
		t.Fatal(err)
	}
	s.refreshEnv = false
	t.Setenv("PATH", "/terminal/bin:/usr/bin")
	t.Setenv("TMUX_TMPDIR", "/terminal/tmux")
	if err := s.Install(); err != nil {
		t.Fatal(err)
	}
	if fake.bootouts != 0 || !strings.Contains(fake.definition, "/login/bin:/usr/bin") || strings.Contains(fake.definition, "/terminal") {
		t.Fatalf("bootouts = %d; definition:\n%s", fake.bootouts, fake.definition)
	}
	if got := changedKeys(map[string]string{"PATH": "a", "HOME": "h", "OLD": "x"}, map[string]string{"PATH": "b", "HOME": "h", "NEW": "y"}); !slices.Equal(got, []string{"NEW", "OLD", "PATH"}) {
		t.Fatalf("changedKeys = %v", got)
	}
}

func mustRender(t *testing.T, bin string) string {
	t.Helper()
	data, err := renderLaunchd(configForOS(bin, "darwin"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
