package tui

import (
	"image/color"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/labstack/driga/internal/view"
)

func TestTerminalBackgroundUpdatesTheme(t *testing.T) {
	original := view.HasDarkBackground()
	t.Cleanup(func() { view.SetDarkBackground(original) })
	m := setup(t, fixture(), Options{}).(*app)
	for _, tc := range []struct {
		background color.Color
		dark       bool
	}{{color.White, false}, {color.Black, true}} {
		_, cmd := m.Update(tea.BackgroundColorMsg{Color: tc.background})
		if cmd != nil || view.HasDarkBackground() != tc.dark {
			t.Fatalf("terminal background did not update theme without IO: dark=%t command=%t", view.HasDarkBackground(), cmd != nil)
		}
	}
}

func TestExplicitThemeIgnoresBackground(t *testing.T) {
	original := view.HasDarkBackground()
	t.Cleanup(func() { _ = view.SetTheme("system"); view.SetDarkBackground(original) })
	var colors [2][4]uint32
	for i, theme := range []string{"dark", "light"} {
		m := setup(t, fixture(), Options{Theme: theme}).(*app)
		for _, background := range []color.Color{color.White, color.Black} {
			_, _ = m.Update(tea.BackgroundColorMsg{Color: background})
			if view.HasDarkBackground() != (theme == "dark") {
				t.Fatal("explicit theme lost")
			}
		}
		r, g, b, a := view.ThemeColor("ink").RGBA()
		colors[i] = [4]uint32{r, g, b, a}
	}
	if colors[0] == colors[1] {
		t.Fatal("whole-TUI ink did not switch")
	}
}
