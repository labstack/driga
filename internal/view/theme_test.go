package view

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
)

func TestThemeCSSContainsEveryToken(t *testing.T) {
	css := ThemeCSS()
	sections := strings.Split(css, "}")
	if len(ThemeTokens()) != 23 {
		t.Fatalf("token count: %d", len(ThemeTokens()))
	}
	required := strings.Fields("ground surface sunk ink muted faint line strong on-strong working working-bg waiting waiting-bg waiting-bar stalled stalled-bg idle idle-bg finished finished-bg crit warn ok")
	for _, name := range required {
		if !strings.Contains(css, "--"+name+":") {
			t.Errorf("missing required token %s", name)
		}
	}
	for _, token := range ThemeTokens() {
		for i, value := range []string{token.Light, token.Dark} {
			if !strings.Contains(sections[i], "--"+token.Name+":"+value+";") {
				t.Errorf("theme %d missing %s=%s", i, token.Name, value)
			}
		}
	}
	if !strings.Contains(css, `prefers-color-scheme: dark`) || !strings.Contains(css, `[data-theme="light"]`) || !strings.Contains(css, `[data-theme="dark"]`) {
		t.Fatal("missing explicit or system theme")
	}
}

func TestThemeTerminalParity(t *testing.T) {
	original := compat.HasDarkBackground
	defer func() { compat.HasDarkBackground = original }()
	for _, token := range ThemeTokens() {
		adaptive := ThemeColor(token.Name)
		for _, dark := range []bool{false, true} {
			compat.HasDarkBackground = dark
			expected := token.Light
			if dark {
				expected = token.Dark
			}
			r, g, b, a := adaptive.RGBA()
			er, eg, eb, ea := lipgloss.Color(expected).RGBA()
			if r != er || g != eg || b != eb || a != ea {
				t.Errorf("%s dark=%v differs from CSS", token.Name, dark)
			}
		}
	}
}

func TestConsoleThemeTokenParity(t *testing.T) {
	css := ThemeCSS()
	// A high-specificity legacy system selector must never override console tokens
	// (especially paper selected while the OS uses dark mode).
	if strings.Contains(css, `:root:not([data-theme="light"]):not([data-theme="dark"])`) {
		t.Error("legacy automatic dark selector overrides named console palettes")
	}

	for _, set := range []string{
		`--bg:#0A0E13;--pane:#0E141B;--raise:#151D27;--line:#233040;--fg:#D9E1EA;--mid:#93A1B3;--dim:#5D6B7D;--accent:#FFB547;--on-accent:#1A1206;--working:#4BD08A;--waiting:#F5B84B;--stalled:#FF7A6E;--idle:#78A6F5;--finished:#9AA6B6;--crit:#FF6F62;--warn:#F5B84B;`,
		`--bg:#F1F2EE;--pane:#FAFAF7;--raise:#ECEDE7;--line:#C9CDC4;--fg:#1D211F;--mid:#4E5650;--dim:#7C847E;--accent:#B5590A;--on-accent:#FFFFFF;--working:#17784A;--waiting:#9A5B00;--stalled:#B3302A;--idle:#2B5BB8;--finished:#5E6660;--crit:#B3302A;--warn:#9A5B00;`,
	} {
		if !strings.Contains(css, set) {
			t.Errorf("missing exact console token set %s", set)
		}
	}
	for _, selector := range []string{`[data-theme="phosphor"]`, `[data-theme="paper"]`, `prefers-color-scheme: light`} {
		if !strings.Contains(css, selector) {
			t.Errorf("missing %s", selector)
		}
	}
}
