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
