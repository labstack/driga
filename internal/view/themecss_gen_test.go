package view

import (
	"os"
	"testing"
)

// The panels UI inlines the console palette: MCP App hosts allow no network,
// so it cannot load /static/theme.css. DRIGA_WRITE_THEME=1 regenerates it.
func TestPanelsThemeMatchesConsole(t *testing.T) {
	const path = "../../ui/src/panels/theme.css"
	want := "/* Generated from internal/view.ThemeCSS; run DRIGA_WRITE_THEME=1 go test ./internal/view. */\n" + ThemeCSS()
	if os.Getenv("DRIGA_WRITE_THEME") == "1" {
		if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("%s is out of date; run DRIGA_WRITE_THEME=1 go test ./internal/view", path)
	}
}
