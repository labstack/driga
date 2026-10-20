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
	if len(ThemeTokens()) != 25 {
		t.Fatalf("token count: %d", len(ThemeTokens()))
	}
	required := strings.Fields("ground surface sunk ink muted faint line strong on-strong working working-bg waiting waiting-bg waiting-bar stalled stalled-bg idle idle-bg finished finished-bg crit warn ok accent observation")
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
	// (especially latte selected while the OS uses dark mode).
	if strings.Contains(css, `:root:not([data-theme="light"]):not([data-theme="dark"])`) {
		t.Error("legacy automatic dark selector overrides named console palettes")
	}

	// Catppuccin Mocha (official) and Latte (text roles darkened for 4.5:1).
	for _, set := range []string{
		`--bg:#1E1E2E;--pane:#181825;--raise:#313244;--line:#45475A;--fg:#CDD6F4;--mid:#BAC2DE;--dim:#A6ADC8;--accent:#CBA6F7;--on-accent:#1E1E2E;--working:#A6E3A1;--waiting:#F9E2AF;--stalled:#F38BA8;--idle:#89B4FA;--finished:#A6ADC8;--crit:#F38BA8;--warn:#FAB387;`,
		`--bg:#EFF1F5;--pane:#E6E9EF;--raise:#CCD0DA;--line:#BCC0CC;--fg:#4C4F69;--mid:#5C5F77;--dim:#5C5F77;--accent:#7629D6;--on-accent:#EFF1F5;--working:#256817;--waiting:#7A4C07;--stalled:#B50C30;--idle:#1A5AD8;--finished:#5C5F77;--crit:#B50C30;--warn:#963F06;`,
	} {
		if !strings.Contains(css, set) {
			t.Errorf("missing exact console token set %s", set)
		}
	}
	for _, selector := range []string{`[data-theme="mocha"]`, `[data-theme="latte"]`, `prefers-color-scheme: light`} {
		if !strings.Contains(css, selector) {
			t.Errorf("missing %s", selector)
		}
	}
}

// An unknown token must render plain text, never a nil color: rendering a
// zero AdaptiveColor panics inside Lip Gloss and kills the TUI.
func TestUnknownTokenRendersPlain(t *testing.T) {
	if got := TokenStyle("no-such-token", false).Render("x"); got != "x" {
		t.Fatalf("got %q", got)
	}
}

// Every token the TUI asks for must exist in the shared palette.
func TestTUITokensExist(t *testing.T) {
	for _, name := range []string{"accent", "crit", "faint", "idle", "ink", "line", "muted", "working", "on-strong", "strong", "sunk"} {
		found := false
		for _, tok := range ThemeTokens() {
			found = found || tok.Name == name
		}
		if !found {
			t.Errorf("token %q missing", name)
		}
	}
}
