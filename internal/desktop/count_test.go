package desktop

import "testing"

func TestCountLabel(t *testing.T) {
	for n, want := range map[int]string{-1: "", 0: "", 1: "1", 9: "9", 10: "9+", 120: "9+"} {
		if got := CountLabel(n); got != want {
			t.Errorf("CountLabel(%d) = %q; want %q", n, got, want)
		}
	}
}

func TestTrayLook(t *testing.T) {
	for _, tc := range []struct {
		goos           string
		n              int
		dark           bool
		title, tooltip string
		icon           string
		template       bool
	}{
		{"darwin", 0, false, "", "Driga", "tray-template.png", true},
		{"darwin", 3, true, "3", "Driga · 3 need you", "tray-template.png", true},
		{"darwin", 12, false, "9+", "Driga · 12 need you", "tray-template.png", true},
		{"windows", 0, true, "", "Driga", "tray-white.png", false},
		{"windows", 2, true, "", "Driga · 2 need you", "tray-white-dot.png", false},
		{"windows", 1, false, "", "Driga · 1 needs you", "tray-ink-dot.png", false},
		{"linux", 0, false, "", "Driga", "tray-white.png", false},
		{"linux", 4, false, "", "Driga · 4 need you", "tray-white-dot.png", false},
	} {
		look := trayLook(tc.goos, tc.n, tc.dark)
		if look.Title != tc.title || look.ToolTip != tc.tooltip || look.Icon != tc.icon || look.Template != tc.template {
			t.Errorf("trayLook(%s, %d, %v) = %+v", tc.goos, tc.n, tc.dark, look)
		}
		if _, err := assets.ReadFile("assets/" + look.Icon); err != nil {
			t.Errorf("icon %s: %v", look.Icon, err)
		}
	}
}
