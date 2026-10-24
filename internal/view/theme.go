package view

import (
	"fmt"
	"image/color"
	"strings"
	"sync/atomic"

	"charm.land/lipgloss/v2"
)

// ThemeToken is one semantic color in the browser or terminal palette.
type ThemeToken struct{ Name, Light, Dark string }

// Markdown roles share names and colors across terminal and browser output.
var markdownTokens = []ThemeToken{
	{"md-heading", "#7629D6", "#CBA6F7"},
	{"md-code", "#4C4F69", "#CDD6F4"},
	{"md-code-bg", "#DCE0E8", "#11111B"},
	{"md-link", "#1A5AD8", "#89B4FA"},
	{"md-quote", "#5C5F77", "#A6ADC8"},
	{"md-rule", "#5C5F77", "#A6ADC8"},
	{"md-table-border", "#5C5F77", "#A6ADC8"},
	{"md-strike", "#5C5F77", "#A6ADC8"},
	{"md-task", "#256817", "#A6E3A1"},
}

// Catppuccin palettes (MIT): Latte for light, Mocha for dark. Mocha uses the
// official colors. Latte's yellow, green, blue, peach and overlay grays fall
// below 4.5:1 for text on its base, so those text roles use darker shades of
// the same hue; bars and fills reuse them.
var themeTokens = append([]ThemeToken{
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
	{"ended", "#5C5F77", "#A6ADC8"},
}, markdownTokens...)

// consoleTokens maps the console's roles onto the same Catppuccin palettes.
var consoleTokens = append([]ThemeToken{
	{"flow-canvas", "#EFF1F5", "#0E0E16"}, {"flow-panel", "#E6E9EF", "#10101A"}, {"flow-line", "#BCC0CC", "#262636"}, {"flow-muted", "#5C5F77", "#7F849C"},
	{"bg", "#EFF1F5", "#1E1E2E"}, {"pane", "#E6E9EF", "#181825"},
	{"raise", "#CCD0DA", "#313244"}, {"line", "#BCC0CC", "#45475A"},
	{"fg", "#4C4F69", "#CDD6F4"}, {"mid", "#5C5F77", "#BAC2DE"},
	{"dim", "#5C5F77", "#A6ADC8"}, {"accent", "#7629D6", "#CBA6F7"},
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
}, markdownTokens...)

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

// Background detection belongs to the interactive event loop. Import-time
// terminal queries can block indefinitely when a Windows MCP client has no console.
var lightBackground atomic.Bool
var themePreference atomic.Int32

// SetTheme selects a palette for the entire TUI. Auto follows background events.
func SetTheme(theme string) error {
	switch theme {
	case "", "auto":
		themePreference.Store(0)
	case "mocha":
		themePreference.Store(1)
	case "latte":
		themePreference.Store(2)
	default:
		return fmt.Errorf("theme must be auto, mocha or latte")
	}
	return nil
}

// HasDarkBackground reports the last TUI observation, or the default dark theme.
func HasDarkBackground() bool {
	switch themePreference.Load() {
	case 1:
		return true
	case 2:
		return false
	default:
		return !lightBackground.Load()
	}
}

// SetDarkBackground records an asynchronous terminal background response.
func SetDarkBackground(dark bool) { lightBackground.Store(!dark) }

type adaptiveColor struct{ light, dark color.Color }

func (c adaptiveColor) RGBA() (uint32, uint32, uint32, uint32) {
	if !HasDarkBackground() {
		return c.light.RGBA()
	}
	return c.dark.RGBA()
}

// ThemeColor adapts to the background reported by the TUI, defaulting to dark.
func ThemeColor(name string) color.Color {
	for _, t := range themeTokens {
		if t.Name == name {
			return adaptiveColor{light: lipgloss.Color(t.Light), dark: lipgloss.Color(t.Dark)}
		}
	}
	return lipgloss.NoColor{}
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
