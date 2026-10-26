package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
)

func inboxRows() []view.InboxRow {
	return []view.InboxRow{
		{MachineID: "peer", InboxItem: model.InboxItem{ID: "w1", SessionID: 7}},
		{MachineID: "local", InboxItem: model.InboxItem{ID: "w1", SessionID: 42}},
		{MachineID: "local", InboxItem: model.InboxItem{ID: "health", SessionID: 0}},
	}
}

type fakePage struct {
	shown int
	evals []string
}

func (p *fakePage) Show()          { p.shown++ }
func (p *fakePage) Eval(js string) { p.evals = append(p.evals, js) }

type fakeWindows struct {
	pages  []*fakePage
	loaded []func()
}

func (f *fakeWindows) create(loaded func()) page {
	p := &fakePage{}
	f.pages = append(f.pages, p)
	f.loaded = append(f.loaded, loaded)
	return p
}

func TestWindowHostReopensAfterClose(t *testing.T) {
	windows := &fakeWindows{}
	host := &windowHost{create: windows.create}
	host.Command("view", "machines")
	windows.loaded[0]()
	host.Closed(windows.pages[0])
	// A stale load of the closed window runs nothing.
	host.Command("view", "fleet")
	windows.loaded[0]()
	if len(windows.pages) != 2 || len(windows.pages[1].evals) != 0 {
		t.Fatalf("stale load ran: %+v", windows.pages[1])
	}
	windows.loaded[1]()
	if !slices.Equal(windows.pages[1].evals, []string{`window.drigaCommand("view","fleet")`}) {
		t.Fatalf("evals %q", windows.pages[1].evals)
	}
	if !host.IsOpen() {
		t.Error("host reports no window")
	}
	host.Closed(windows.pages[1])
	if host.IsOpen() {
		t.Error("host reports a closed window")
	}
}

func TestWindowRules(t *testing.T) {
	for _, tc := range []struct {
		goos string
		tray bool
		want bool
	}{{"darwin", true, false}, {"darwin", false, false}, {"windows", true, false}, {"windows", false, true}, {"linux", false, true}, {"linux", true, false}} {
		if got := CloseQuits(tc.goos, tc.tray); got != tc.want {
			t.Errorf("CloseQuits(%s, %v) = %v", tc.goos, tc.tray, got)
		}
	}
	// The Dock icon hides only while the menu bar icon stands in for it.
	if !dockRegular(true, true) || !dockRegular(false, false) || dockRegular(false, true) {
		t.Error("dock policy")
	}
	// A login launch starts hidden only where an icon is left to open it from.
	for _, tc := range []struct {
		goos              string
		login, tray, want bool
	}{
		{"darwin", true, true, true},
		{"darwin", true, false, true},
		{"windows", true, false, false},
		{"windows", true, true, true},
		{"linux", false, true, false},
	} {
		if got := startHidden(tc.goos, tc.login, tc.tray); got != tc.want {
			t.Errorf("startHidden(%s, %v, %v) = %v", tc.goos, tc.login, tc.tray, got)
		}
	}
}

func TestUpgradeWatch(t *testing.T) {
	stamp := time.Unix(100, 0)
	version, readErr := "v1", error(nil)
	reads := 0
	w := &upgradeWatch{
		exe: "/app/driga", version: "v1",
		stat: func(string) (time.Time, error) { return stamp, nil },
		read: func(context.Context, string) (string, error) { reads++; return version, readErr },
	}
	ctx := context.Background()
	w.start()
	if w.changed(ctx) || reads != 0 {
		t.Fatal("unchanged executable read or reported")
	}
	stamp = time.Unix(200, 0)
	readErr = errors.New("text file busy")
	if w.changed(ctx) {
		t.Fatal("unreadable executable reported")
	}
	readErr = nil
	if w.changed(ctx) || reads != 2 {
		t.Fatalf("same version reported (reads %d)", reads)
	}
	// The same version at the new time is remembered: no more reads.
	if w.changed(ctx) || reads != 2 {
		t.Fatalf("re-read an unchanged executable (reads %d)", reads)
	}
	stamp, version = time.Unix(300, 0), "v2"
	if !w.changed(ctx) {
		t.Fatal("new version not reported")
	}
}

// A window create reaches back into the host, as the toolkit's activation and
// close listeners do while the window opens: nothing deadlocks, and no second
// window is made.
func TestWindowHostCreateReentry(t *testing.T) {
	var host *windowHost
	var pages []*fakePage
	var loads []func()
	host = &windowHost{create: func(loaded func()) page {
		p := &fakePage{}
		pages, loads = append(pages, p), append(loads, loaded)
		if len(pages) == 1 {
			host.Open()
			host.Command("view", "fleet")
			if host.IsOpen() || host.Current() != nil {
				t.Error("a window being created counts as open")
			}
		}
		return p
	}}
	within(t, host.Open)
	if len(pages) != 1 || host.Current() != pages[0] {
		t.Fatalf("windows %d, current %v", len(pages), host.Current())
	}
	loads[0]()
	if !slices.Equal(pages[0].evals, []string{`window.drigaCommand("view","fleet")`}) {
		t.Fatalf("evals %q", pages[0].evals)
	}
}

// A window closed while it was being created is not kept; the next Open
// makes a new one.
func TestWindowHostClosedDuringCreate(t *testing.T) {
	var host *windowHost
	var pages []*fakePage
	host = &windowHost{create: func(func()) page {
		p := &fakePage{}
		pages = append(pages, p)
		if len(pages) == 1 {
			host.Closed(p)
		}
		return p
	}}
	within(t, func() { host.Command("view", "fleet") })
	if host.IsOpen() {
		t.Fatal("kept a closed window")
	}
	host.Open()
	if len(pages) != 2 || host.Current() != pages[1] {
		t.Fatalf("windows %d", len(pages))
	}
}

// within fails the test when f does not return, as when it deadlocks.
func within(t *testing.T, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the host deadlocked")
	}
}

// A bundle's version is read from its Info.plist, never by running the
// executable, which can wait on Gatekeeper after an upgrade.
func TestInstalledVersionReadsBundle(t *testing.T) {
	contents := filepath.Join(t.TempDir(), "Driga.app", "Contents")
	if err := os.MkdirAll(filepath.Join(contents, "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
	<key>LSEnvironment</key><dict><key>DrigaReleaseVersion</key><string>nested</string></dict>
	<key>CFBundleVersion</key><string>2026.10.17</string>
	<key>DrigaReleaseVersion</key><string>2026.10.17-alpha</string>
</dict></plist>`
	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	// No executable exists: running it would fail.
	got, err := installedVersion(context.Background(), filepath.Join(contents, "MacOS", "driga"))
	if err != nil || got != "2026.10.17-alpha" {
		t.Fatalf("installedVersion = %q, %v", got, err)
	}
	if _, err := plistString(strings.NewReader(plist), "Missing"); err == nil {
		t.Fatal("a missing key was found")
	}
}

// Collector restarts wait while the executable changed and its version is
// not read yet.
func TestUpgradeWatchPending(t *testing.T) {
	stamp := time.Unix(100, 0)
	w := &upgradeWatch{
		exe: "/app/driga", version: "v1",
		stat: func(string) (time.Time, error) { return stamp, nil },
		read: func(context.Context, string) (string, error) { return "v1", nil },
	}
	w.start()
	if w.pending() {
		t.Fatal("unchanged executable pending")
	}
	stamp = time.Unix(200, 0)
	if !w.pending() {
		t.Fatal("replaced executable not pending")
	}
	if w.changed(context.Background()) || w.pending() {
		t.Fatal("same version still pending after it was read")
	}
}
