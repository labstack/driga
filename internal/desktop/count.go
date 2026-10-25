package desktop

import (
	"embed"
	"strconv"
)

//go:embed assets/*.png
var assets embed.FS

// CountLabel is the needs-you count as the tray title shows it.
func CountLabel(n int) string {
	switch {
	case n <= 0:
		return ""
	case n > 9:
		return "9+"
	}
	return strconv.Itoa(n)
}

// look is how the tray icon shows the count.
type look struct {
	Title, ToolTip string
	Icon           string
	Template       bool
}

// trayLook shows the count as the macOS menu bar title. Notification areas
// have no title, so there the icon gains a dot and the tooltip says the count.
// Their icons are drawn in color, light on a dark bar; Linux panels report no
// color and are usually dark.
func trayLook(goos string, n int, dark bool) look {
	l := look{ToolTip: "Driga"}
	if n > 0 {
		l.ToolTip += " · " + needLabel(n)
	}
	if goos == "darwin" {
		l.Title, l.Icon, l.Template = CountLabel(n), "tray-template.png", true
		return l
	}
	tone := "ink"
	if dark || goos != "windows" {
		tone = "white"
	}
	l.Icon = "tray-" + tone + ".png"
	if n > 0 {
		l.Icon = "tray-" + tone + "-dot.png"
	}
	return l
}
