package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// A service the app could not remove keeps collecting, and the tray says how
// to remove it.
func TestServiceRemovalFailureInTray(t *testing.T) {
	failed := errors.New("launchctl failed")
	dataDir := t.TempDir()
	s := &shell{goos: "darwin", homes: sameHomes, openService: func() (installedService, error) {
		return &fakeService{installed: true, dataDir: dataDir, uninstallErr: failed}, nil
	}}
	if removed := s.removeService(dataDir); removed {
		t.Fatal("reported a removal that failed")
	}
	for _, st := range []Status{{}, {Collecting: true}} {
		s.status = st
		if line := StatusLine(s.trayStatus()); !strings.Contains(line, "run driga service uninstall") {
			t.Errorf("status %+v: tray says %q", st, line)
		}
	}
	// The supervisor's own error comes first.
	s.status = Status{Err: errors.New("collector exited")}
	if line := StatusLine(s.trayStatus()); line != "Collector error: collector exited" {
		t.Errorf("tray says %q", line)
	}
}

func TestOpenAtLoginRegistersOnChangeOnly(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	var calls []bool
	set := func(open bool) error { calls = append(calls, open); return nil }
	// First launch, then a later one with the setting unchanged: the user
	// may have turned the login item off in System Settings since.
	for range 2 {
		if err := syncOpenAtLogin(dataDir, true, set); err != nil {
			t.Fatal(err)
		}
	}
	if len(calls) != 1 || !calls[0] {
		t.Fatalf("registered %v; want once, on", calls)
	}
	// Changing the setting registers again.
	if err := syncOpenAtLogin(dataDir, false, set); err != nil {
		t.Fatal(err)
	}
	if err := syncOpenAtLogin(dataDir, false, set); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[1] {
		t.Fatalf("registered %v; want on, then off", calls)
	}
	// A failed registration is tried again on the next launch.
	failed := errors.New("not allowed")
	if err := syncOpenAtLogin(dataDir, true, func(bool) error { return failed }); !errors.Is(err, failed) {
		t.Fatalf("err = %v", err)
	}
	if err := syncOpenAtLogin(dataDir, true, set); err != nil || len(calls) != 3 {
		t.Fatalf("not retried: %v %v", calls, err)
	}
}

// fakeCollector counts the collector runs the shell starts and keeps each
// running until it is stopped.
type fakeCollector struct {
	mu      sync.Mutex
	runs    int
	running int
}

func (f *fakeCollector) run(ctx context.Context) {
	f.mu.Lock()
	f.runs++
	f.running++
	f.mu.Unlock()
	<-ctx.Done()
	f.mu.Lock()
	f.running--
	f.mu.Unlock()
}

func (f *fakeCollector) state() (runs, running int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.runs, f.running
}

func (f *fakeCollector) waitRunning(t *testing.T, runs int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if r, n := f.state(); r == runs && n == 1 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	r, n := f.state()
	t.Fatalf("collector runs %d, running %d; want %d, 1", r, n, runs)
}

func TestRelaunchCancelledKeepsCollecting(t *testing.T) {
	os.Unsetenv("DRIGA_APP")
	t.Cleanup(func() { os.Unsetenv("DRIGA_APP") })
	collector := &fakeCollector{}
	var during string
	s := &shell{runCollector: collector.run, relaunchApp: func() bool {
		if _, running := collector.state(); running != 0 {
			t.Error("relaunched while the old collector ran")
		}
		during = os.Getenv("DRIGA_APP")
		return false
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.ctx = ctx
	s.startCollector(nil)
	collector.waitRunning(t, 1)
	s.relaunch()
	if during != "1" {
		t.Fatalf("relaunched with DRIGA_APP=%q", during)
	}
	if value, set := os.LookupEnv("DRIGA_APP"); set {
		t.Fatalf("DRIGA_APP=%q left set after the cancelled quit", value)
	}
	collector.waitRunning(t, 2)
	s.stopCollector()
	if _, running := collector.state(); running != 0 {
		t.Fatal("collector still runs after stop")
	}
}

func TestRelaunchKeepsALoginLaunchMarked(t *testing.T) {
	t.Setenv("DRIGA_APP", "1")
	s := &shell{runCollector: (&fakeCollector{}).run, relaunchApp: func() bool { return false }, ctx: context.Background()}
	s.relaunch()
	s.stopCollector()
	if os.Getenv("DRIGA_APP") != "1" {
		t.Fatal("DRIGA_APP the app started with was removed")
	}
}

// An app in an isolated home says nothing about the service in the tray.
func TestIsolatedHomeHasNoServiceHint(t *testing.T) {
	s := &shell{goos: "darwin", homes: func() (string, string) { return "/tmp/fake", "/Users/someone" },
		openService: func() (installedService, error) { t.Fatal("opened the service"); return nil, nil }}
	if s.removeService(t.TempDir()) {
		t.Fatal("removed")
	}
	if line := StatusLine(s.trayStatus()); line != "Starting the collector…" {
		t.Fatalf("tray says %q", line)
	}
}

// Once the app's final stop has run, nothing starts the collector again.
func TestNoCollectorAfterQuit(t *testing.T) {
	collector := &fakeCollector{}
	s := &shell{runCollector: collector.run, ctx: context.Background()}
	s.startCollector(nil)
	collector.waitRunning(t, 1)
	s.finish()
	s.startCollector(nil)
	time.Sleep(50 * time.Millisecond)
	if runs, running := collector.state(); runs != 1 || running != 0 {
		t.Fatalf("collector runs %d, running %d after quit", runs, running)
	}
}
