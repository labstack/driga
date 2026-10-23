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
