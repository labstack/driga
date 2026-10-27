package service

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ks "github.com/kardianos/service"
	"github.com/labstack/driga/internal/collector"
)

var lifecycleFailure = errors.New("injected service failure")

// A local registration/process adapter, never the account's OS registration.
// Running the service command enters Control just as the real subprocess does.
type testLifecycleHost struct {
	platform, binaryVersion, fail string
	installed, legacy, held, live bool
	registrationRevision          int
	process                       collector.Owner
	output                        io.Writer
}

func (h *testLifecycleHost) controller() (Controller, error) { return testController{h}, nil }
func (h *testLifecycleHost) run(ctx context.Context, action string) error {
	l := NewLifecycle(LifecycleOptions{Platform: h.platform, Output: h.output})
	l.host = h
	return l.Control(ctx, action)
}
func (h *testLifecycleHost) owner() (collector.Owner, bool, error) {
	if h.fail == "owner" {
		return collector.Owner{}, false, lifecycleFailure
	}
	return h.process, h.held, nil
}
func (h *testLifecycleHost) alive() bool { return h.live }
func (h *testLifecycleHost) prepareLog() error {
	if h.fail == "log" {
		return lifecycleFailure
	}
	return nil
}
func (h *testLifecycleHost) removeLegacy() error {
	if h.fail == "legacy" {
		return lifecycleFailure
	}
	h.legacy = false
	return nil
}

type testController struct{ h *testLifecycleHost }

func (c testController) Install() error {
	h := c.h
	if h.fail == "install" {
		return lifecycleFailure
	}
	if h.installed && h.platform != "darwin" {
		return errors.New("registration already exists")
	}
	h.installed = true
	h.registrationRevision++
	return nil
}
func (c testController) Start() error {
	if c.h.fail == "start" {
		return lifecycleFailure
	}
	if !c.h.installed {
		return ks.ErrNotInstalled
	}
	if !c.h.live {
		c.h.process = collector.Owner{PID: 42, Version: c.h.binaryVersion, Kind: "service"}
		c.h.held, c.h.live = true, true
	}
	return nil
}
func (c testController) Stop() error { c.h.held, c.h.live = false, false; return nil }
func (c testController) Restart() error {
	if c.h.fail == "restart" {
		return lifecycleFailure
	}
	_ = c.Stop()
	return c.Start()
}
func (c testController) Uninstall() error { c.h.installed = false; return c.Stop() }
func (c testController) Status() (ks.Status, error) {
	if c.h.fail == "probe" {
		return ks.StatusUnknown, lifecycleFailure
	}
	if !c.h.installed {
		return ks.StatusUnknown, ks.ErrNotInstalled
	}
	if c.h.live {
		return ks.StatusRunning, nil
	}
	return ks.StatusStopped, nil
}

func testLifecycle(platform string) (*Lifecycle, *testLifecycleHost) {
	l := NewLifecycle(LifecycleOptions{Platform: platform, Version: "v2", Home: "/account", AccountHome: "/account"})
	h := &testLifecycleHost{platform: platform, binaryVersion: "v2", output: io.Discard}
	l.host = h
	return l, h
}

func TestLifecycleEnsureObservedRegistrationAndVersion(t *testing.T) {
	for _, platform := range []string{"darwin", "linux", "windows"} {
		for _, initial := range []string{"absent", "stopped", "current", "old"} {
			t.Run(platform+"/"+initial, func(t *testing.T) {
				l, h := testLifecycle(platform)
				if initial != "absent" {
					h.installed, h.registrationRevision = true, 1
				}
				if initial == "current" || initial == "old" {
					h.live, h.held = true, true
					h.process = collector.Owner{PID: 7, Kind: "service", Version: "v2"}
					if initial == "old" {
						h.process.Version = "v1"
					}
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if err := l.Ensure(ctx); err != nil {
					t.Fatal(err)
				}
				st := l.Observe()
				if st.Err != nil || !st.Installed || !st.Running || st.Owner.Version != "v2" {
					t.Fatalf("observed outcome: %+v", st)
				}
				if want := 1; platform == "darwin" && initial != "absent" {
					if h.registrationRevision != want+1 {
						t.Fatal("macOS registration was not refreshed")
					}
				} else if h.registrationRevision != want {
					t.Fatal("existing registration was replaced")
				}
				cancel() // Closing the app's context never stops drigad.
				if !l.Observe().Running {
					t.Fatal("drigad depended on the app lifetime")
				}
			})
		}
	}
}

func TestLifecycleFailuresDescribeObservedState(t *testing.T) {
	for _, stage := range []string{"log", "install", "legacy", "start"} {
		t.Run(stage, func(t *testing.T) {
			l, h := testLifecycle("linux")
			h.fail, h.legacy = stage, true
			if err := l.Ensure(context.Background()); !errors.Is(err, lifecycleFailure) {
				t.Fatalf("startup failure: %v", err)
			}
			st := l.Observe()
			if !errors.Is(st.Err, lifecycleFailure) || st.Running || st.Installed != (stage == "legacy" || stage == "start") || h.legacy != (stage != "start") {
				t.Fatalf("failed %s: %+v; legacy=%t", stage, st, h.legacy)
			}
		})
	}
	for _, stage := range []string{"probe", "start", "owner", "restart"} {
		t.Run("old/"+stage, func(t *testing.T) {
			l, h := testLifecycle("linux")
			h.fail, h.installed, h.live, h.held = stage, true, true, true
			h.process = collector.Owner{PID: 7, Version: "v1"}
			if err := l.Ensure(context.Background()); !errors.Is(err, lifecycleFailure) {
				t.Fatal(err)
			}
			st := l.Observe()
			if !errors.Is(st.Err, lifecycleFailure) || !h.live || h.process.Version != "v1" {
				t.Fatalf("lost the previous process or startup error: %+v", st)
			}
		})
	}
}

type failingLifecycleOutput struct{}

func (failingLifecycleOutput) Write([]byte) (int, error) { return 0, lifecycleFailure }

func TestLifecycleControlAndStatus(t *testing.T) {
	l, h := testLifecycle("linux")
	h.output = failingLifecycleOutput{}
	if err := l.Ensure(context.Background()); !errors.Is(err, lifecycleFailure) {
		t.Fatal(err)
	}
	if st := l.Observe(); !st.Installed || st.Running {
		t.Fatalf("hint write failure lost installed state: %+v", st)
	}
	h.output = io.Discard
	if err := l.Control(context.Background(), "start"); err != nil {
		t.Fatal(err)
	}
	h.held = false
	if l.Observe().Running {
		t.Fatal("process without writer lock reported running")
	}
	h.held, h.live = true, false
	if l.Observe().Running {
		t.Fatal("stale lock owner reported running")
	}
	if err := l.Control(context.Background(), "uninstall"); err != nil {
		t.Fatal(err)
	}
	if st := l.Observe(); st.Installed || st.Running || st.Registration != "not installed" {
		t.Fatalf("uninstall observation: %+v", st)
	}
}

func TestLifecycleIsolation(t *testing.T) {
	l, h := testLifecycle("linux")
	l.opts.Home = "/isolated"
	h.installed, h.live, h.held, h.legacy = true, true, true, true
	h.process = collector.Owner{PID: 7, Version: "v1"}
	if err := l.Ensure(context.Background()); !errors.Is(err, ErrIsolatedHome) {
		t.Fatal(err)
	}
	if st := l.Observe(); st.Err != nil || !st.Running || st.Owner.Version != "v1" || !h.legacy {
		t.Fatalf("isolated client changed the account's service: %+v", st)
	}
	l.opts.Home = ""
	if err := l.Ensure(context.Background()); err != nil {
		t.Fatalf("unset HOME is isolated: %v", err)
	}
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "home")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("no symlinks:", err)
	}
	l.opts.Home, l.opts.AccountHome = link, real
	if err := l.Ensure(context.Background()); err != nil {
		t.Fatalf("a symlinked home is isolated: %v", err)
	}
}

func TestLifecycleStatusDiagnostics(t *testing.T) {
	base := Status{Running: true}
	d := Diagnostics{EnvironmentError: lifecycleFailure, Shadowed: []string{"DRIGA_DATA_DIR"}}
	if st := base.WithDiagnostics(d); !errors.Is(st.Err, lifecycleFailure) || !strings.Contains(st.Err.Error(), "without login shell PATH") {
		t.Fatal(st)
	}
	base.Err = errors.New("startup failed")
	if st := base.WithDiagnostics(d); st.Err != base.Err {
		t.Fatal("environment error replaced startup error")
	}
	d.RestartPending = true
	if st := base.WithDiagnostics(d); !st.RestartPending || st.Err != nil {
		t.Fatal("app restart did not take precedence")
	}
}
