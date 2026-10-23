package view

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestThemeCSSContainsEveryToken(t *testing.T) {
	css := ThemeCSS()
	sections := strings.Split(css, "}")
	for _, token := range consoleTokens {
		for i, value := range []string{token.Dark, token.Light} {
			if !strings.Contains(sections[i], "--"+token.Name+":"+value+";") {
				t.Errorf("theme %d missing %s=%s", i, token.Name, value)
			}
		}
	}
}

func TestThemeTerminalParity(t *testing.T) {
	original := lightBackground.Load()
	defer lightBackground.Store(original)
	for _, token := range themeTokens {
		adaptive := ThemeColor(token.Name)
		for _, dark := range []bool{false, true} {
			SetDarkBackground(dark)
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
	// Catppuccin Mocha (official) and Latte (text roles darkened for 4.5:1).
	for _, set := range []string{
		`--bg:#1E1E2E;--pane:#181825;--raise:#313244;--line:#45475A;--fg:#CDD6F4;--mid:#BAC2DE;--dim:#A6ADC8;--accent:#CBA6F7;--on-accent:#1E1E2E;--working:#A6E3A1;--waiting:#F9E2AF;--stalled:#F38BA8;--idle:#89B4FA;--finished:#A6ADC8;--crit:#F38BA8;--warn:#FAB387;`,
		`--bg:#EFF1F5;--pane:#E6E9EF;--raise:#CCD0DA;--line:#BCC0CC;--fg:#4C4F69;--mid:#5C5F77;--dim:#6C6F85;--accent:#7629D6;--on-accent:#EFF1F5;--working:#256817;--waiting:#7A4C07;--stalled:#B50C30;--idle:#1A5AD8;--finished:#5C5F77;--crit:#B50C30;--warn:#963F06;`,
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
	for _, name := range []string{"accent", "crit", "faint", "idle", "ink", "line", "muted", "working", "on-strong", "strong", "sunk", "waiting", "stalled", "finished", "warn", "ok", "observation", "tool"} {
		found := false
		for _, tok := range themeTokens {
			found = found || tok.Name == name
		}
		if !found {
			t.Errorf("token %q missing", name)
		}
	}
}

func TestFlowMockTokens(t *testing.T) {
	for _, want := range []ThemeToken{
		{"waiting-bar", "#DF8E1D", "#F9E2AF"}, {"waiting-bg", "#F3E7D3", "#3A3530"},
		{"tool", "#04778A", "#89DCEB"}, {"rail", "#A9AEBC", "#585B70"},
	} {
		found := false
		for _, got := range consoleTokens {
			if got.Name == want.Name {
				found = true
				if got != want {
					t.Errorf("%s: %+v", want.Name, got)
				}
			}
		}
		if !found {
			t.Error("missing console token", want.Name)
		}
	}
}

// Every palette variable used by web assets must have a console value.
func TestWebAssetsUseConsoleTokens(t *testing.T) {
	console := map[string]bool{}
	for _, tok := range consoleTokens {
		console[tok.Name] = true
	}
	files, err := filepath.Glob("../web/static/*.[cj]s*")
	if err != nil || len(files) == 0 {
		t.Fatalf("static assets: %v %v", files, err)
	}
	ref := regexp.MustCompile(`var\(--([a-z0-9-]+)`)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		// Fonts and component-local meter/state variables are defined by the assets.
		for _, m := range ref.FindAllStringSubmatch(string(b), -1) {
			if !console[m[1]] && m[1] != "mono" && m[1] != "sans" && m[1] != "meter-fill" && m[1] != "state" {
				t.Errorf("%s uses --%s, which has no console value", filepath.Base(f), m[1])
			}
		}
	}
}
