package view

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
)

// ThemeToken is one semantic color in the browser or terminal palette.
type ThemeToken struct{ Name, Light, Dark string }

// Catppuccin palettes (MIT): Latte for light, Mocha for dark. Mocha uses the
// official colors. Latte's yellow, green, blue, peach and overlay grays fall
// below 4.5:1 for text on its base, so those text roles use darker shades of
// the same hue; bars and fills reuse them.
var themeTokens = [...]ThemeToken{
	{"sunk", "#DCE0E8", "#11111B"},
	{"ink", "#4C4F69", "#CDD6F4"},
	{"muted", "#5C5F77", "#A6ADC8"},
	{"faint", "#5C5F77", "#A6ADC8"},
	{"line", "#CCD0DA", "#313244"},
	{"strong", "#4C4F69", "#CDD6F4"},
	{"on-strong", "#EFF1F5", "#1E1E2E"},
	{"working", "#256817", "#A6E3A1"},
	{"waiting", "#7A4C07", "#F9E2AF"},
	{"stalled", "#B50C30", "#F38BA8"},
	{"idle", "#1A5AD8", "#89B4FA"},
	{"finished", "#5C5F77", "#A6ADC8"},
	{"crit", "#B50C30", "#F38BA8"},
	{"warn", "#963F06", "#FAB387"},
	{"ok", "#1A5AD8", "#89B4FA"},
	{"accent", "#7629D6", "#CBA6F7"},
	{"observation", "#116762", "#94E2D5"},
	{"tool", "#04778A", "#89DCEB"},
}

// consoleTokens maps the console's roles onto the same Catppuccin palettes.
var consoleTokens = [...]ThemeToken{
	{"bg", "#EFF1F5", "#1E1E2E"}, {"pane", "#E6E9EF", "#181825"},
	{"raise", "#CCD0DA", "#313244"}, {"line", "#BCC0CC", "#45475A"},
	{"fg", "#4C4F69", "#CDD6F4"}, {"mid", "#5C5F77", "#BAC2DE"},
	{"dim", "#6C6F85", "#A6ADC8"}, {"accent", "#7629D6", "#CBA6F7"},
	{"on-accent", "#EFF1F5", "#1E1E2E"}, {"working", "#256817", "#A6E3A1"},
	{"waiting", "#7A4C07", "#F9E2AF"}, {"stalled", "#B50C30", "#F38BA8"},
	{"idle", "#1A5AD8", "#89B4FA"}, {"finished", "#5C5F77", "#A6ADC8"},
	{"crit", "#B50C30", "#F38BA8"}, {"warn", "#963F06", "#FAB387"},
	{"observation", "#116762", "#94E2D5"},
	{"waiting-bar", "#DF8E1D", "#F9E2AF"}, {"waiting-bg", "#F3E7D3", "#3A3530"},
	{"tool", "#04778A", "#89DCEB"}, {"rail", "#A9AEBC", "#585B70"},
	{"faint", "#5C5F77", "#A6ADC8"},
	{"working-bg", "#D3E4D5", "#364143"}, {"stalled-bg", "#EACDD7", "#443244"},
	{"idle-bg", "#CEDBF5", "#313953"}, {"finished-bg", "#DADCE3", "#333446"},
}

// ThemeCSS supports named preferences and the system default without script.
func ThemeCSS() string {
	var latte, mocha strings.Builder
	for _, t := range consoleTokens {
		fmt.Fprintf(&latte, "--%s:%s;", t.Name, t.Light)
		fmt.Fprintf(&mocha, "--%s:%s;", t.Name, t.Dark)
	}
	return `:root,[data-theme="mocha"]{` + mocha.String() + "color-scheme:dark;}\n" +
		`[data-theme="latte"]{` + latte.String() + "color-scheme:light;}\n" +
		`@media (prefers-color-scheme: light){:root:not([data-theme="mocha"]):not([data-theme="latte"]){` + latte.String() + "color-scheme:light;}}\n"
}

// ThemeColor adapts to Lip Gloss's terminal background detection.
func ThemeColor(name string) compat.AdaptiveColor {
	for _, t := range themeTokens {
		if t.Name == name {
			return compat.AdaptiveColor{Light: lipgloss.Color(t.Light), Dark: lipgloss.Color(t.Dark)}
		}
	}
	return compat.AdaptiveColor{}
}

// TokenStyle colors text with a shared token. An unknown name renders plain:
// a zero AdaptiveColor would panic inside Lip Gloss and take the TUI down.
func TokenStyle(name string, noColor bool) lipgloss.Style {
	s := lipgloss.NewStyle()
	if noColor {
		return s
	}
	for _, t := range themeTokens {
		if t.Name == name {
			return s.Foreground(ThemeColor(name))
		}
	}
	return s
}
