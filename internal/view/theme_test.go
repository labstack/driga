package view

import (
	"fmt"
	catppuccin "github.com/catppuccin/go"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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

func TestConsoleThemeSelectors(t *testing.T) {
	css := ThemeCSS()
	for _, selector := range []string{`[data-theme="dark"]`, `[data-theme="light"]`, `prefers-color-scheme: light`} {
		if !strings.Contains(css, selector) {
			t.Errorf("missing %s", selector)
		}
	}
}

// Dark uses the official Mocha colors for every state; light text roles keep
// the official Latte hue and are only as much darker as 4.5:1 requires.
func TestThemeFollowsCatppuccinAndStaysReadable(t *testing.T) {
	rgb := func(h string) [3]uint8 {
		var c [3]uint8
		fmt.Sscanf(h, "#%02X%02X%02X", &c[0], &c[1], &c[2])
		return c
	}
	official := map[string][2]catppuccin.Color{
		"working": {catppuccin.Latte.Green(), catppuccin.Mocha.Green()},
		"waiting": {catppuccin.Latte.Yellow(), catppuccin.Mocha.Yellow()},
		"stalled": {catppuccin.Latte.Red(), catppuccin.Mocha.Red()},
		"idle":    {catppuccin.Latte.Blue(), catppuccin.Mocha.Blue()},
		"warn":    {catppuccin.Latte.Peach(), catppuccin.Mocha.Peach()},
		"accent":  {catppuccin.Latte.Mauve(), catppuccin.Mocha.Mauve()},
		"tool":    {catppuccin.Latte.Sky(), catppuccin.Mocha.Sky()},
	}
	for _, tok := range consoleTokens {
		want, ok := official[tok.Name]
		if !ok {
			continue
		}
		if tok.Dark != strings.ToUpper(want[1].Hex) {
			t.Errorf("%s dark %s, official Mocha %s", tok.Name, tok.Dark, want[1].Hex)
		}
		// Light text sits on Latte's base and on its darker mantle panels.
		if c := Contrast(rgb(tok.Light), catppuccin.Latte.Mantle().RGB); c < 4.5 {
			t.Errorf("%s light %s contrast %.2f", tok.Name, tok.Light, c)
		}
		if c := Contrast(rgb(tok.Dark), catppuccin.Mocha.Base().RGB); c < 4.5 {
			t.Errorf("%s dark %s contrast %.2f", tok.Name, tok.Dark, c)
		}
		// Passing official colors are used unchanged.
		if Contrast(want[0].RGB, catppuccin.Latte.Mantle().RGB) >= 4.5 && tok.Light != strings.ToUpper(want[0].Hex) {
			t.Errorf("%s light %s should be official %s", tok.Name, tok.Light, want[0].Hex)
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

func TestChatLightMetadataContrast(t *testing.T) {
	colors := map[string]string{}
	for _, token := range consoleTokens {
		colors[token.Name] = token.Light
	}
	luminance := func(hex string) float64 {
		n, err := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 32)
		if err != nil {
			t.Fatal(err)
		}
		channel := func(c uint64) float64 {
			v := float64(c) / 255
			if v <= 0.04045 {
				return v / 12.92
			}
			return math.Pow((v+0.055)/1.055, 2.4)
		}
		return .2126*channel(n>>16&255) + .7152*channel(n>>8&255) + .0722*channel(n&255)
	}
	for _, bg := range []string{"bg", "pane"} {
		ratio := (luminance(colors[bg]) + .05) / (luminance(colors["dim"]) + .05)
		if ratio < 4.5 {
			t.Fatalf("dim on %s: %.2f:1", bg, ratio)
		}
		t.Logf("dim on %s: %.2f:1", bg, ratio)
	}
}
