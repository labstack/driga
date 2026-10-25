package desktop

import "embed"

//go:embed assets/*.png
var assets embed.FS

// look is how the tray icon shows the count.
type look struct {
	ToolTip  string
	Icon     string
	Template bool
}

// trayLook shows an icon only, never text: the icon gains a dot while
// anything needs you, and the tooltip and menu say how many. The macOS menu
// bar takes a template that it tints; notification areas draw icons in color,
// light on a dark bar, and Linux panels report no color and are usually dark.
func trayLook(goos string, n int, dark bool) look {
	l := look{ToolTip: "Driga"}
	if n > 0 {
		l.ToolTip += " · " + needLabel(n)
	}
	if goos == "darwin" {
		l.Icon, l.Template = "tray-template.png", true
		if n > 0 {
			l.Icon = "tray-template-dot.png"
		}
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
