package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUITheme(t *testing.T) {
	if Default().UI.Theme != "auto" {
		t.Fatal("wrong default")
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	for _, value := range []string{"'auto'", "'mocha'", "'latte'", "'dark'", "''", "true", "1"} {
		if err := os.WriteFile(path, []byte("[ui]\ntheme = "+value), 0600); err != nil {
			t.Fatal(err)
		}
		c, err := Load(path)
		valid := value == "'auto'" || value == "'mocha'" || value == "'latte'"
		if (err == nil) != valid {
			t.Fatalf("%s => %+v %v", value, c, err)
		}
	}
	if err := os.WriteFile(path, []byte("[ui]\ntheme = 'latte'"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DRIGA_UI_THEME", "mocha")
	c, err := Load(path)
	if err != nil || c.UI.Theme != "mocha" {
		t.Fatal(c, err)
	}
	c, err = LoadWithOverrides(path, map[string]any{"ui.theme": "auto"})
	if err != nil || c.UI.Theme != "auto" {
		t.Fatal(c, err)
	}
	if _, err = LoadWithOverrides(path, map[string]any{"ui.theme": "bad"}); err == nil {
		t.Fatal("invalid override accepted")
	}
}
