package desktop

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/view"
)

// find returns the item labelled label in menu, searching submenus.
func find(menu []MenuSpec, label string) (MenuSpec, bool) {
	for _, item := range menu {
		if item.Label == label {
			return item, true
		}
		if found, ok := find(item.Submenu, label); ok {
			return found, true
		}
	}
	return MenuSpec{}, false
}

func TestMenuModelTopLevel(t *testing.T) {
	for goos, want := range map[string][]string{
		"darwin":  {"Driga", "File", "Edit", "View", "Window", "Help"},
		"windows": {"Driga", "File", "Edit", "View", "Window", "Help"},
		"linux":   {"Driga", "File", "Edit", "View", "Window", "Help"},
	} {
		var got []string
		for _, item := range MenuModel(goos) {
			got = append(got, item.Label)
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: menus %v; want %v", goos, got, want)
		}
	}
}

func TestMenuModelAccelerators(t *testing.T) {
	for goos, mod := range map[string]string{"darwin": "Cmd", "windows": "Ctrl", "linux": "Ctrl"} {
		menu := MenuModel(goos)
		for label, want := range map[string]struct{ key, command string }{
			"Settings…":     {mod + "+,", CommandSettings},
			"Back":          {mod + "+[", CommandBack},
			"Forward":       {mod + "+]", CommandForward},
			"Reload":        {mod + "+R", CommandReload},
			"Find":          {mod + "+F", CommandFind},
			"Documentation": {"", CommandDocs},
		} {
			item, ok := find(menu, label)
			if !ok {
				t.Errorf("%s: no %q item", goos, label)
				continue
			}
			if item.Accelerator != want.key || item.Command != want.command {
				t.Errorf("%s: %q = %q %q; want %q %q", goos, label, item.Accelerator, item.Command, want.key, want.command)
			}
		}
	}
}

func TestMenuModelRoles(t *testing.T) {
	for _, goos := range []string{"darwin", "windows", "linux"} {
		menu := MenuModel(goos)
		roles := map[string]bool{}
		var walk func([]MenuSpec)
		walk = func(items []MenuSpec) {
			for _, item := range items {
				if item.Role != "" {
					roles[item.Role] = true
				}
				walk(item.Submenu)
			}
		}
		walk(menu)
		for _, role := range []string{"quit", "undo", "redo", "cut", "copy", "paste", "selectAll", "resetZoom", "zoomIn", "zoomOut", "windowMenu"} {
			if !roles[role] {
				t.Errorf("%s: missing role %s", goos, role)
			}
		}
		// Cmd is the Windows key off macOS: every shortcut there uses Ctrl.
		var keys func([]MenuSpec)
		keys = func(items []MenuSpec) {
			for _, item := range items {
				if goos != "darwin" && strings.Contains(item.Accelerator, "Cmd") {
					t.Errorf("%s: %q uses Cmd", goos, item.Label)
				}
				keys(item.Submenu)
			}
		}
		keys(menu)
	}
	if MenuModel("darwin")[0].Role != "appMenu" {
		t.Error("macOS app menu is not the application menu")
	}
	if _, ok := find(MenuModel("darwin"), "About Driga"); !ok {
		t.Error("no About item")
	}
}

func TestTrayMenuModel(t *testing.T) {
	var needs []view.BoardRow
	for i := 1; i <= 7; i++ {
		needs = append(needs, view.BoardRow{Title: fmt.Sprintf("session %d", i), Node: view.FlowNode{ID: int64(i)}})
	}
	menu := TrayMenuModel(needs, Status{Running: true})
	var labels []string
	for _, item := range menu {
		if !item.Separator {
			labels = append(labels, item.Label)
		}
	}
	want := []string{"Open Driga", "7 need you", "session 1", "session 2", "session 3", "session 4", "session 5", "Driga service running", "Settings…", "About Driga", "Quit"}
	if strings.Join(labels, "|") != strings.Join(want, "|") {
		t.Fatalf("tray menu %q; want %q", labels, want)
	}
	if item, _ := find(menu, "session 3"); item.Command != "session:3" {
		t.Errorf("need opens %q", item.Command)
	}
	if item, _ := find(menu, "Driga service running"); !item.Disabled {
		t.Error("status line is clickable")
	}
	if item, _ := find(menu, "Quit"); item.Role != "quit" {
		t.Error("Quit has no quit role")
	}
	empty := TrayMenuModel(nil, Status{})
	if _, ok := find(empty, "0 need you"); ok || len(empty) != 8 {
		t.Errorf("empty tray menu %+v", empty)
	}
}

func TestStatusLine(t *testing.T) {
	for _, tc := range []struct {
		status Status
		want   string
	}{
		{Status{Running: true}, "Driga service running"},
		{Status{}, "Starting the Driga service…"},
		{Status{RestartPending: true, Running: true}, "Restart Driga to finish updating"},
		{Status{Err: errors.New("driga service install: denied")}, "Driga service: driga service install: denied"},
		{Status{Running: true, Err: errors.New("login shell: timeout")}, "Driga service running · login shell: timeout"},
		{Status{Err: errors.New(strings.Repeat("x", 200))}, "Driga service: " + strings.Repeat("x", 64) + "…"},
	} {
		if got := StatusLine(tc.status); got != tc.want {
			t.Errorf("StatusLine(%+v) = %q; want %q", tc.status, got, tc.want)
		}
	}
}

func TestConfirmDialog(t *testing.T) {
	text, detail, buttons := confirmDialog("Stop session “fix tests”? Driga checks it is still the same idle process first.")
	if text != "Stop session “fix tests”?" || detail != "Driga checks it is still the same idle process first." || strings.Join(buttons, ",") != "Stop,Cancel" {
		t.Errorf("stop dialog %q %q %v", text, detail, buttons)
	}
	text, detail, buttons = confirmDialog("Full auto runs every action without asking. Turn it on?")
	if text != "Full auto runs every action without asking. Turn it on?" || detail != "" || strings.Join(buttons, ",") != "OK,Cancel" {
		t.Errorf("generic dialog %q %q %v", text, detail, buttons)
	}
}

func TestExternalLinks(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://driga.dev/docs":        true,
		"http://example.com/":           true,
		"http://mygo.localhost/flow":    false,
		"mygo://localhost/flow?id=4":    false,
		"/flow":                         false,
		"javascript:alert(1)":           false,
		"https://mygo.localhost.evil/x": true,
	} {
		if got := external(raw); got != want {
			t.Errorf("external(%q) = %v; want %v", raw, got, want)
		}
	}
}

// Forged origins and non-web schemes must not replace the privileged app page
// or be handed to the operating system's external opener.
func TestNavigationFailsClosed(t *testing.T) {
	for _, raw := range []string{
		"file:///etc/passwd", "custom://launch", "javascript:alert(1)",
		"data:text/html,hello", "about:blank", "/flow", "//evil.example/app/",
		"mygo://evil.example/app/", "mygo://localhost.evil/app/",
		"mygo://localhost:123/app/", "mygo://user@localhost/app/",
		"http://mygo.localhost:123/app/", "http://user@mygo.localhost/app/",
		"https://mygo.localhost/app/", "http://mygo.localhost.evil/app/", "%zz",
	} {
		if internalNavigation(raw) {
			t.Errorf("forged navigation allowed: %q", raw)
		}
	}
	for _, raw := range []string{"file:///etc/passwd", "custom://launch", "javascript:alert(1)", "mygo://evil.example/app/", "http://user@mygo.localhost/app/"} {
		if external(raw) {
			t.Errorf("unsafe URL sent to external opener: %q", raw)
		}
	}
	for _, raw := range []string{"mygo://localhost/app/", "http://mygo.localhost/app/?view=fleet#session"} {
		if !internalNavigation(raw) || external(raw) {
			t.Errorf("app origin blocked or opened externally: %q", raw)
		}
	}
}
