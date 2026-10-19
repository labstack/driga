package view

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
)

// ThemeToken is one semantic color shared by browser and terminal readers.
type ThemeToken struct{ Name, Light, Dark string }

var themeTokens = [...]ThemeToken{
	{"ground", "#F4F6F9", "#0D1016"},
	{"surface", "#FFFFFF", "#141922"},
	{"sunk", "#EEF1F5", "#10141B"},
	{"ink", "#141821", "#E7EBF2"},
	{"muted", "#566074", "#9AA4B6"},
	{"faint", "#8A93A5", "#6B7486"},
	{"line", "#E1E5EC", "#232A36"},
	{"strong", "#141821", "#E7EBF2"},
	{"on-strong", "#FFFFFF", "#0D1016"},
	{"working", "#18804F", "#45C985"},
	{"working-bg", "#E2F4EA", "#11301F"},
	{"waiting", "#9A5B00", "#F2B451"},
	{"waiting-bg", "#FFF0D2", "#33260E"},
	{"waiting-bar", "#E59A1E", "#F2B451"},
	{"stalled", "#B7322A", "#FF7B70"},
	{"stalled-bg", "#FDE4E1", "#3A1714"},
	{"idle", "#2C5FBF", "#7FA8F5"},
	{"idle-bg", "#E3ECFB", "#152645"},
	{"finished", "#5F6878", "#A3ACBC"},
	{"finished-bg", "#ECEEF2", "#1E2430"},
	{"crit", "#C93A31", "#FF6B5F"},
	{"warn", "#D98A14", "#F2B451"},
	{"ok", "#2C5FBF", "#7FA8F5"},
}

// consoleTokens preserves the console mockup's roles without changing TUI colors.
var consoleTokens = [...]ThemeToken{
	{"bg", "#F1F2EE", "#0A0E13"}, {"pane", "#FAFAF7", "#0E141B"},
	{"raise", "#ECEDE7", "#151D27"}, {"line", "#C9CDC4", "#233040"},
	{"fg", "#1D211F", "#D9E1EA"}, {"mid", "#4E5650", "#93A1B3"},
	{"dim", "#7C847E", "#5D6B7D"}, {"accent", "#B5590A", "#FFB547"},
	{"on-accent", "#FFFFFF", "#1A1206"}, {"working", "#17784A", "#4BD08A"},
	{"waiting", "#9A5B00", "#F5B84B"}, {"stalled", "#B3302A", "#FF7A6E"},
	{"idle", "#2B5BB8", "#78A6F5"}, {"finished", "#5E6660", "#9AA6B6"},
	{"crit", "#B3302A", "#FF6F62"}, {"warn", "#9A5B00", "#F5B84B"},
}

func consoleCSS() string {
	var paper, phosphor strings.Builder
	for _, t := range consoleTokens {
		fmt.Fprintf(&paper, "--%s:%s;", t.Name, t.Light)
		fmt.Fprintf(&phosphor, "--%s:%s;", t.Name, t.Dark)
	}
	return `:root,[data-theme="phosphor"]{` + phosphor.String() + "color-scheme:dark;}\n" +
		`[data-theme="paper"]{` + paper.String() + "color-scheme:light;}\n" +
		`@media (prefers-color-scheme: light){:root:not([data-theme="phosphor"]):not([data-theme="paper"]){` + paper.String() + "color-scheme:light;}}\n"
}

// ThemeTokens returns a copy so callers cannot change the shared palette.
func ThemeTokens() []ThemeToken { return append([]ThemeToken(nil), themeTokens[:]...) }

// ThemeCSS supports explicit preferences and the console system default without script.
// Keep the legacy dark media rule explicit-only so it cannot override console colors.
func ThemeCSS() string {
	var light, dark strings.Builder
	for _, t := range themeTokens {
		fmt.Fprintf(&light, "--%s:%s;", t.Name, t.Light)
		fmt.Fprintf(&dark, "--%s:%s;", t.Name, t.Dark)
	}
	return ":root,[data-theme=\"light\"]{" + light.String() + "color-scheme:light;}\n" +
		"[data-theme=\"dark\"]{" + dark.String() + "color-scheme:dark;}\n" +
		"@media (prefers-color-scheme: dark){:root[data-theme=\"dark\"]{" + dark.String() + "color-scheme:dark;}}\n" + consoleCSS()
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

func TokenStyle(name string, noColor bool) lipgloss.Style {
	s := lipgloss.NewStyle()
	if !noColor {
		s = s.Foreground(ThemeColor(name))
	}
	return s
}
