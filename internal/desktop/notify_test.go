package desktop

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/attention"
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

func TestClickTarget(t *testing.T) {
	for _, tc := range []struct {
		n    attention.Notification
		id   int64
		want bool
	}{
		{attention.Notification{ItemID: "w1", MachineID: "local"}, 42, true},
		{attention.Notification{ItemID: "w1", MachineID: "peer"}, 7, true},
		{attention.Notification{ItemID: "w1", MachineID: "other"}, 0, false},
		{attention.Notification{ItemID: "health", MachineID: "local"}, 0, false},
		{attention.Notification{Kind: "waiting"}, 0, false},
	} {
		id, ok := ClickTarget(tc.n, inboxRows())
		if id != tc.id || ok != tc.want {
			t.Errorf("ClickTarget(%+v) = %d %v; want %d %v", tc.n, id, ok, tc.id, tc.want)
		}
	}
}

func TestNotificationIDRoundTrip(t *testing.T) {
	for _, n := range []attention.Notification{
		{Kind: "waiting", ItemID: "a/b c", MachineID: "local"},
		{Kind: "finished", ItemID: "x", MachineID: ""},
		{Kind: "waiting"},
	} {
		id := notificationID(n)
		if !strings.HasPrefix(id, "driga:") {
			t.Fatalf("id %q has no app prefix", id)
		}
		got, ok := parseNotificationID(id)
		if !ok || got.ItemID != n.ItemID || got.MachineID != n.MachineID {
			t.Errorf("round trip %+v → %q → %+v %v", n, id, got, ok)
		}
	}
	// One per item: a later notification about the same item replaces it.
	if notificationID(attention.Notification{ItemID: "a", MachineID: "m", Title: "one"}) != notificationID(attention.Notification{ItemID: "a", MachineID: "m", Title: "two"}) {
		t.Error("same item, different IDs")
	}
	if _, ok := parseNotificationID("other-app:1"); ok {
		t.Error("foreign ID parsed")
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

// Review Focus 4: a click with no window open creates the window and selects
// the session once its page has loaded.
func TestNotificationClickWithoutWindow(t *testing.T) {
	windows := &fakeWindows{}
	host := &windowHost{create: windows.create}
	id := notificationID(attention.Notification{ItemID: "w1", MachineID: "local"})
	openNotification(host, id, inboxRows)
	if len(windows.pages) != 1 {
		t.Fatalf("created %d windows", len(windows.pages))
	}
	p := windows.pages[0]
	if len(p.evals) != 0 {
		t.Fatalf("ran %q before the page loaded", p.evals)
	}
	windows.loaded[0]()
	want := `window.drigaCommand("openSession",42)`
	if !slices.Equal(p.evals, []string{want}) {
		t.Fatalf("evals %q; want %q", p.evals, want)
	}
	// Later loads of the same page (navigations) do not repeat the command.
	windows.loaded[0]()
	if len(p.evals) != 1 {
		t.Fatalf("command repeated: %q", p.evals)
	}
}

func TestNotificationClickWithWindow(t *testing.T) {
	windows := &fakeWindows{}
	host := &windowHost{create: windows.create}
	host.Open()
	windows.loaded[0]()
	p := windows.pages[0]
	openNotification(host, notificationID(attention.Notification{ItemID: "w1", MachineID: "peer"}), inboxRows)
	if len(windows.pages) != 1 || p.shown != 1 {
		t.Fatalf("windows %d, shown %d", len(windows.pages), p.shown)
	}
	if !slices.Equal(p.evals, []string{`window.drigaCommand("openSession",7)`}) {
		t.Fatalf("evals %q", p.evals)
	}
	// A summary or health notification only brings the window forward.
	openNotification(host, notificationID(attention.Notification{Kind: "waiting"}), inboxRows)
	if len(p.evals) != 1 || p.shown != 2 {
		t.Fatalf("summary click: evals %q shown %d", p.evals, p.shown)
	}
}

func TestWindowHostReopensAfterClose(t *testing.T) {
	windows := &fakeWindows{}
	host := &windowHost{create: windows.create}
	host.Command("view", "machines")
	windows.loaded[0]()
	host.Closed(windows.pages[0])
	// A stale load of the closed window runs nothing.
	host.Command("view", "flow")
	windows.loaded[0]()
	if len(windows.pages) != 2 || len(windows.pages[1].evals) != 0 {
		t.Fatalf("stale load ran: %+v", windows.pages[1])
	}
	windows.loaded[1]()
	if !slices.Equal(windows.pages[1].evals, []string{`window.drigaCommand("view","flow")`}) {
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
