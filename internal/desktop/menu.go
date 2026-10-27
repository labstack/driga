package desktop

import (
	"fmt"
	"github.com/labstack/driga/internal/service"
	"strings"

	"github.com/labstack/driga/internal/view"
)

// Commands a menu item can carry. "view:<name>" switches the page's view,
// "go:<path>" opens one of its pages and "session:<id>" opens a session.
const (
	CommandAbout    = "about"
	CommandSettings = "settings"
	CommandOpen     = "open"
	CommandFind     = "find"
	CommandReload   = "reload"
	CommandBack     = "back"
	CommandForward  = "forward"
	CommandDocs     = "docs"
)

// DocsURL is what Help → Documentation opens in the browser.
const DocsURL = "https://driga.dev"

// trayNeeds is how many needs the tray menu lists.
const trayNeeds = 5

// MenuSpec describes a menu item without the toolkit, so the menus are
// tested without a display. Role names a mygo role; Command is what the app
// does when the item is chosen.
type MenuSpec struct {
	Label       string
	Role        string
	Accelerator string
	Command     string
	Disabled    bool
	Separator   bool
	Submenu     []MenuSpec
}

var separator = MenuSpec{Separator: true}

// MenuModel is the menu bar. Shortcuts use Cmd on macOS and Ctrl elsewhere,
// where Cmd would be the Windows or Super key.
func MenuModel(goos string) []MenuSpec {
	mod := "Ctrl"
	if goos == "darwin" {
		mod = "Cmd"
	}
	key := func(k string) string { return mod + "+" + k }
	app := MenuSpec{Label: "Driga", Submenu: []MenuSpec{
		{Label: "About Driga", Command: CommandAbout},
		separator,
		{Label: "Settings…", Accelerator: key(","), Command: CommandSettings},
		separator,
		{Role: "quit"},
	}}
	if goos == "darwin" {
		// The macOS application menu, with the items every Mac app has.
		app.Role = "appMenu"
		app.Submenu = []MenuSpec{
			{Label: "About Driga", Command: CommandAbout},
			separator,
			{Label: "Settings…", Accelerator: key(","), Command: CommandSettings},
			separator,
			{Role: "services"},
			separator,
			{Role: "hide"},
			{Role: "hideOthers"},
			{Role: "unhide"},
			separator,
			{Role: "quit"},
		}
	}
	edit := MenuSpec{Label: "Edit", Submenu: []MenuSpec{
		{Role: "undo"},
		{Role: "redo"},
		separator,
		{Role: "cut"},
		{Role: "copy"},
		{Role: "paste"},
		{Role: "selectAll"},
		separator,
		{Label: "Find", Accelerator: key("F"), Command: CommandFind},
	}}
	file := MenuSpec{Label: "File", Submenu: []MenuSpec{
		{Label: "Close Window", Role: "close", Accelerator: key("W")},
	}}
	viewMenu := MenuSpec{Label: "View", Submenu: []MenuSpec{
		{Label: "Back", Accelerator: key("["), Command: CommandBack},
		{Label: "Forward", Accelerator: key("]"), Command: CommandForward},
		{Label: "Reload", Accelerator: key("R"), Command: CommandReload},
		separator,
		{Role: "resetZoom"},
		{Role: "zoomIn"},
		{Role: "zoomOut"},
		separator,
		{Role: "togglefullscreen"},
	}}
	help := MenuSpec{Label: "Help", Submenu: []MenuSpec{
		{Label: "Documentation", Command: CommandDocs},
	}}
	if goos == "darwin" {
		help.Role = "help"
	}
	return []MenuSpec{app, file, edit, viewMenu, {Label: "Window", Role: "windowMenu"}, help}
}

// TrayMenuModel is the tray icon's menu: Open Driga, the count and the first
// needs, each opening its session, the collector's status, Settings, About
// and Quit.
func TrayMenuModel(needs []view.BoardRow, status service.Status) []MenuSpec {
	menu := []MenuSpec{{Label: "Open Driga", Command: CommandOpen}, separator}
	if len(needs) > 0 {
		menu = append(menu, MenuSpec{Label: needLabel(len(needs)), Disabled: true})
		for _, need := range needs[:min(trayNeeds, len(needs))] {
			menu = append(menu, MenuSpec{Label: clip(need.Title, 48), Command: fmt.Sprintf("session:%d", need.Node.ID)})
		}
		menu = append(menu, separator)
	}
	return append(menu, MenuSpec{Label: StatusLine(status), Disabled: true}, separator,
		MenuSpec{Label: "Settings…", Command: CommandSettings}, MenuSpec{Label: "About Driga", Command: CommandAbout}, separator,
		MenuSpec{Label: "Quit", Role: "quit"})
}

func needLabel(n int) string {
	if n == 1 {
		return "1 needs you"
	}
	return fmt.Sprintf("%d need you", n)
}

// StatusLine says whether the Driga service runs, or why it does not.
func StatusLine(st service.Status) string {
	switch {
	case st.RestartPending:
		return "Restart Driga to finish updating"
	case st.Running && st.Err != nil:
		return clip("Driga service running · "+st.Err.Error(), 80)
	case st.Running:
		return "Driga service running"
	case st.Err != nil:
		return clip("Driga service: "+st.Err.Error(), 80)
	}
	return "Starting the Driga service…"
}

// clip shortens s to n runes, marking the cut.
func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
