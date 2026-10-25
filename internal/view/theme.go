package view

import (
	"encoding/json"
	"fmt"
	"image/color"
	"math"
	"strings"
	"sync/atomic"

	"charm.land/lipgloss/v2"
	catppuccin "github.com/catppuccin/go"
)

// ThemeToken is one semantic color in the browser or terminal palette.
type ThemeToken struct{ Name, Light, Dark string }

// Palette roles come from the official Catppuccin palettes (MIT): Latte for
// light, Mocha for dark. Where an official Latte color falls below 4.5:1 for
// text on Latte's mantle, the panel surface and darker than base, text roles
// use readable(), the same hue darkened just enough, so they pass on page and
// panels alike. Tinted backgrounds are tint() mixes of the state color into base.
var (
	lb, mb = catppuccin.Latte.Base(), catppuccin.Mocha.Base()
	pair   = func(l, d catppuccin.Color) [2]string { return [2]string{hex(l), hex(d)} }
	text   = func(l, d catppuccin.Color) [2]string {
		return [2]string{readable(l, catppuccin.Latte.Mantle()), hex(d)}
	}
)

func tok(name string, v [2]string) ThemeToken { return ThemeToken{name, v[0], v[1]} }

// Markdown roles share names and colors across terminal and browser output.
var markdownTokens = []ThemeToken{
	tok("md-heading", text(catppuccin.Latte.Mauve(), catppuccin.Mocha.Mauve())),
	tok("md-code", pair(catppuccin.Latte.Text(), catppuccin.Mocha.Text())),
	tok("md-code-bg", pair(catppuccin.Latte.Crust(), catppuccin.Mocha.Crust())),
	tok("md-link", text(catppuccin.Latte.Blue(), catppuccin.Mocha.Blue())),
	tok("md-quote", pair(catppuccin.Latte.Subtext1(), catppuccin.Mocha.Subtext0())),
	tok("md-rule", pair(catppuccin.Latte.Subtext1(), catppuccin.Mocha.Subtext0())),
	tok("md-table-border", pair(catppuccin.Latte.Subtext1(), catppuccin.Mocha.Subtext0())),
	tok("md-strike", pair(catppuccin.Latte.Subtext1(), catppuccin.Mocha.Subtext0())),
	tok("md-task", text(catppuccin.Latte.Green(), catppuccin.Mocha.Green())),
}

// Semantic state roles, shared by the terminal and the browser.
var (
	ink      = pair(catppuccin.Latte.Text(), catppuccin.Mocha.Text())
	muted    = pair(catppuccin.Latte.Subtext1(), catppuccin.Mocha.Subtext0())
	working  = text(catppuccin.Latte.Green(), catppuccin.Mocha.Green())
	waiting  = text(catppuccin.Latte.Yellow(), catppuccin.Mocha.Yellow())
	stalled  = text(catppuccin.Latte.Red(), catppuccin.Mocha.Red())
	idle     = text(catppuccin.Latte.Blue(), catppuccin.Mocha.Blue())
	warn     = text(catppuccin.Latte.Peach(), catppuccin.Mocha.Peach())
	accent   = text(catppuccin.Latte.Mauve(), catppuccin.Mocha.Mauve())
	observed = text(catppuccin.Latte.Teal(), catppuccin.Mocha.Teal())
	tool     = text(catppuccin.Latte.Sky(), catppuccin.Mocha.Sky())
	onBase   = pair(catppuccin.Latte.Base(), catppuccin.Mocha.Base())
)

var themeTokens = append([]ThemeToken{
	tok("sunk", pair(catppuccin.Latte.Crust(), catppuccin.Mocha.Crust())),
	tok("ink", ink), tok("muted", muted), tok("faint", muted),
	tok("line", pair(catppuccin.Latte.Surface0(), catppuccin.Mocha.Surface0())),
	tok("strong", ink), tok("on-strong", onBase),
	tok("working", working), tok("waiting", waiting), tok("stalled", stalled),
	tok("idle", idle), tok("finished", muted), tok("crit", stalled),
	tok("warn", warn), tok("ok", idle), tok("accent", accent),
	tok("observation", observed), tok("tool", tool), tok("ended", muted),
}, markdownTokens...)

// consoleTokens maps the console's roles onto the same palettes. The Flow
// canvas sits one step below Mocha's crust so the project cards stand out.
var consoleTokens = append([]ThemeToken{
	tok("flow-canvas", [2]string{hex(catppuccin.Latte.Base()), mix(catppuccin.Mocha.Crust().RGB, [3]uint8{}, 0.2)}),
	tok("flow-panel", [2]string{hex(catppuccin.Latte.Mantle()), mix(catppuccin.Mocha.Crust().RGB, [3]uint8{}, 0.08)}),
	tok("flow-line", [2]string{hex(catppuccin.Latte.Surface1()), mix(mb.RGB, catppuccin.Mocha.Surface0().RGB, 0.45)}),
	tok("flow-muted", pair(catppuccin.Latte.Subtext1(), catppuccin.Mocha.Overlay1())),
	tok("bg", onBase), tok("pane", pair(catppuccin.Latte.Mantle(), catppuccin.Mocha.Mantle())),
	tok("raise", pair(catppuccin.Latte.Surface0(), catppuccin.Mocha.Surface0())),
	tok("line", pair(catppuccin.Latte.Surface1(), catppuccin.Mocha.Surface1())),
	tok("fg", ink), tok("mid", pair(catppuccin.Latte.Subtext1(), catppuccin.Mocha.Subtext1())),
	tok("dim", muted), tok("accent", accent), tok("on-accent", onBase),
	tok("working", working), tok("waiting", waiting), tok("stalled", stalled),
	tok("idle", idle), tok("finished", muted), tok("crit", stalled), tok("warn", warn),
	tok("observation", observed), tok("tool", tool), tok("faint", muted),
	tok("waiting-bar", pair(catppuccin.Latte.Yellow(), catppuccin.Mocha.Yellow())),
	tok("rail", pair(catppuccin.Latte.Surface2(), catppuccin.Mocha.Surface2())),
	tok("waiting-bg", tint(catppuccin.Latte.Yellow(), catppuccin.Mocha.Yellow())),
	tok("working-bg", tint(catppuccin.Latte.Green(), catppuccin.Mocha.Green())),
	tok("stalled-bg", tint(catppuccin.Latte.Red(), catppuccin.Mocha.Red())),
	tok("idle-bg", tint(catppuccin.Latte.Blue(), catppuccin.Mocha.Blue())),
	tok("finished-bg", tint(catppuccin.Latte.Subtext1(), catppuccin.Mocha.Subtext0())),
}, markdownTokens...)

func hex(c catppuccin.Color) string { return strings.ToUpper(c.Hex) }

// tint mixes a state color into each flavor's base for a soft background.
func tint(l, d catppuccin.Color) [2]string {
	return [2]string{mix(lb.RGB, l.RGB, 0.15), mix(mb.RGB, d.RGB, 0.15)}
}

func mix(a, b [3]uint8, t float64) string {
	var out [3]uint8
	for i := range out {
		out[i] = uint8(math.Round(float64(a[i]) + (float64(b[i])-float64(a[i]))*t))
	}
	return fmt.Sprintf("#%02X%02X%02X", out[0], out[1], out[2])
}

// readable darkens c in small steps, keeping its hue, until text in it
// reaches 4.5:1 (WCAG AA) on bg. Colors that already pass are unchanged.
func readable(c, bg catppuccin.Color) string {
	rgb := c.RGB
	for k := 1.0; k > 0; k -= 0.01 {
		var out [3]uint8
		for i := range out {
			out[i] = uint8(math.Round(float64(c.RGB[i]) * k))
		}
		rgb = out
		if Contrast(rgb, bg.RGB) >= 4.5 {
			break
		}
	}
	return fmt.Sprintf("#%02X%02X%02X", rgb[0], rgb[1], rgb[2])
}

// Contrast is the WCAG contrast ratio of two sRGB colors.
func Contrast(a, b [3]uint8) float64 {
	lum := func(c [3]uint8) float64 {
		var l [3]float64
		for i, v := range c {
			x := float64(v) / 255
			if x <= 0.04045 {
				l[i] = x / 12.92
			} else {
				l[i] = math.Pow((x+0.055)/1.055, 2.4)
			}
		}
		return 0.2126*l[0] + 0.7152*l[1] + 0.0722*l[2]
	}
	x, y := lum(a), lum(b)
	return (max(x, y) + 0.05) / (min(x, y) + 0.05)
}

// ThemeJSON is the palette the app's UI theme is built from: the console
// roles for each scheme, and both Catppuccin flavors by name for the colors
// the UI needs that the console has no role for, such as chart series.
func ThemeJSON() string {
	roles := func(light bool) map[string]string {
		out := map[string]string{}
		for _, t := range consoleTokens {
			out[t.Name] = t.Dark
			if light {
				out[t.Name] = t.Light
			}
		}
		return out
	}
	flavor := func(f catppuccin.Flavor) map[string]string {
		out := map[string]string{}
		for name, c := range map[string]catppuccin.Color{
			"rosewater": f.Rosewater(), "flamingo": f.Flamingo(), "pink": f.Pink(), "mauve": f.Mauve(), "red": f.Red(),
			"maroon": f.Maroon(), "peach": f.Peach(), "yellow": f.Yellow(), "green": f.Green(), "teal": f.Teal(),
			"sky": f.Sky(), "sapphire": f.Sapphire(), "blue": f.Blue(), "lavender": f.Lavender(), "text": f.Text(),
			"subtext1": f.Subtext1(), "subtext0": f.Subtext0(), "overlay2": f.Overlay2(), "overlay1": f.Overlay1(),
			"overlay0": f.Overlay0(), "surface2": f.Surface2(), "surface1": f.Surface1(), "surface0": f.Surface0(),
			"base": f.Base(), "mantle": f.Mantle(), "crust": f.Crust(),
		} {
			out[name] = hex(c)
		}
		return out
	}
	b, _ := json.MarshalIndent(map[string]map[string]string{
		"light": roles(true), "dark": roles(false), "latte": flavor(catppuccin.Latte), "mocha": flavor(catppuccin.Mocha),
	}, "", "  ")
	return string(b) + "\n"
}

// Background detection belongs to the interactive event loop. Import-time
// terminal queries can block indefinitely when a Windows MCP client has no console.
var lightBackground atomic.Bool
var themePreference atomic.Int32

// SetTheme selects a palette for the entire TUI. System follows the terminal background.
func SetTheme(theme string) error {
	switch theme {
	case "", "system":
		themePreference.Store(0)
	case "dark":
		themePreference.Store(1)
	case "light":
		themePreference.Store(2)
	default:
		return fmt.Errorf("theme must be system, light or dark")
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
