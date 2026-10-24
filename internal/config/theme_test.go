package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUITheme(t *testing.T) {
	if Default().UI.Theme != "system" {
		t.Fatal("wrong default")
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	for _, value := range []string{"'system'", "'light'", "'dark'", "'auto'", "'mocha'", "''", "true", "1"} {
		if err := os.WriteFile(path, []byte("[ui]\ntheme = "+value), 0600); err != nil {
			t.Fatal(err)
		}
		c, err := Load(path)
		valid := value == "'system'" || value == "'light'" || value == "'dark'"
		if (err == nil) != valid {
			t.Fatalf("%s => %+v %v", value, c, err)
		}
	}
	if err := os.WriteFile(path, []byte("[ui]\ntheme = 'light'"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DRIGA_UI_THEME", "dark")
	c, err := Load(path)
	if err != nil || c.UI.Theme != "dark" {
		t.Fatal(c, err)
	}
	c, err = LoadWithOverrides(path, map[string]any{"ui.theme": "system"})
	if err != nil || c.UI.Theme != "system" {
		t.Fatal(c, err)
	}
	if _, err = LoadWithOverrides(path, map[string]any{"ui.theme": "bad"}); err == nil {
		t.Fatal("invalid override accepted")
	}
}
