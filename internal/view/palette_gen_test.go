package view

import (
	"os"
	"testing"
)

// The app's UI theme is built from the console palette, inlined because MCP
// App hosts allow no network. DRIGA_WRITE_THEME=1 regenerates it.
func TestUIPaletteMatchesConsole(t *testing.T) {
	const path = "../../ui/src/theme/palette.json"
	want := ThemeJSON()
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
