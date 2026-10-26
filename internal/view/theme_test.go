package view

import (
	"encoding/json"
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

func TestThemeJSONContainsEveryToken(t *testing.T) {
	var palette map[string]map[string]string
	if err := json.Unmarshal([]byte(ThemeJSON()), &palette); err != nil {
		t.Fatal(err)
	}
	for _, token := range consoleTokens {
		if palette["dark"][token.Name] != token.Dark || palette["light"][token.Name] != token.Light {
			t.Errorf("palette missing %s", token.Name)
		}
	}
	if len(palette["mocha"]) != 26 || len(palette["latte"]) != 26 {
		t.Errorf("flavors: %d %d", len(palette["mocha"]), len(palette["latte"]))
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
		"stuck":   {catppuccin.Latte.Red(), catppuccin.Mocha.Red()},
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
	for _, name := range []string{"accent", "crit", "faint", "idle", "ink", "line", "muted", "working", "on-strong", "strong", "sunk", "waiting", "stuck", "finished", "warn", "ok", "observation", "tool"} {
		found := false
		for _, tok := range themeTokens {
			found = found || tok.Name == name
		}
		if !found {
			t.Errorf("token %q missing", name)
		}
	}
}

// The app's colors come from the console palette through ui/src/theme, so its
// style sheets name theme variables and never a color of their own.
func TestAppStylesUseThemeColors(t *testing.T) {
	host, _ := filepath.Glob("../../ui/src/host/*.css")
	files, err := filepath.Glob("../../ui/src/panels/*.css")
	files = append(files, host...)
	if err != nil || len(files) < 2 {
		t.Fatalf("style sheets: %v %v", files, err)
	}
	color := regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b|rgba?\(`)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if m := color.Find(b); m != nil {
			t.Errorf("%s sets %s; use a theme variable", filepath.Base(f), m)
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
