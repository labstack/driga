package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/labstack/driga/internal/web"
)

// A service the app could not install or start shows in the tray, whether
// or not an earlier one still runs.
func TestServiceErrorInTray(t *testing.T) {
	s := &shell{serviceErr: errors.New("driga service install: denied")}
	if line := StatusLine(s.trayStatus()); line != "Driga service: driga service install: denied" {
		t.Errorf("tray says %q", line)
	}
	s.status = Status{Running: true}
	if line := StatusLine(s.trayStatus()); line != "Driga service running · driga service install: denied" {
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

// A cancelled relaunch keeps the app as it is, and the tray says how to
// finish updating.
func TestRelaunchCancelledWaitsForRestart(t *testing.T) {
	os.Unsetenv("DRIGA_APP")
	t.Cleanup(func() { os.Unsetenv("DRIGA_APP") })
	var during string
	s := &shell{relaunchApp: func() bool { during = os.Getenv("DRIGA_APP"); return false }, ctx: context.Background()}
	if s.relaunch() {
		t.Fatal("a cancelled relaunch reported as done")
	}
	if during != "1" {
		t.Fatalf("relaunched with DRIGA_APP=%q", during)
	}
	if value, set := os.LookupEnv("DRIGA_APP"); set {
		t.Fatalf("DRIGA_APP=%q left set after the cancelled quit", value)
	}
	s.status = Status{Running: true}
	if line := StatusLine(s.trayStatus()); line != "Restart Driga to finish updating" {
		t.Fatalf("tray says %q", line)
	}
}

// The watch keeps checking after a cancelled relaunch and asks again.
func TestUpgradeWatchRetriesCancelledRelaunch(t *testing.T) {
	stats := 0
	w := &upgradeWatch{
		exe: "/app/driga", version: "v1", interval: time.Millisecond,
		// Replaced after the watch started.
		stat: func(string) (time.Time, error) { stats++; return time.Unix(int64(min(stats, 2)), 0), nil },
		read: func(context.Context, string) (string, error) { return "v2", nil },
	}
	calls := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.run(context.Background(), func() bool { calls++; return calls == 3 })
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("watch stopped asking")
	}
	if calls != 3 {
		t.Fatalf("relaunch asked %d times, want 3", calls)
	}
}

func TestRelaunchKeepsALoginLaunchMarked(t *testing.T) {
	t.Setenv("DRIGA_APP", "1")
	s := &shell{relaunchApp: func() bool { return false }, ctx: context.Background()}
	s.relaunch()
	if os.Getenv("DRIGA_APP") != "1" {
		t.Fatal("DRIGA_APP the app started with was removed")
	}
}

// An app in an isolated home leaves the login service alone.
func TestIsolatedHomeLeavesTheService(t *testing.T) {
	s := &shell{homes: func() (string, string) { return "/tmp/fake", "/Users/someone" }, serviceExe: "/nonexistent/driga"}
	if err := s.ensureService(context.Background()); !errors.Is(err, errIsolatedHome) {
		t.Fatalf("ensureService: %v", err)
	}
}

// A second launch hands over to the running app before it opens the store,
// which the running app's collector may be migrating.
func TestSecondInstanceDoesNotOpenSource(t *testing.T) {
	opened := 0
	open := func() (web.Source, func(), error) { opened++; return nil, func() {}, nil }
	if _, _, first, err := openAfterLock(func() bool { return false }, open); first || err != nil || opened != 0 {
		t.Fatalf("second instance: first %v, err %v, opened %d", first, err, opened)
	}
	if _, closeSource, first, err := openAfterLock(func() bool { return true }, open); !first || err != nil || opened != 1 || closeSource == nil {
		t.Fatalf("first instance: first %v, err %v, opened %d", first, err, opened)
	}
}
