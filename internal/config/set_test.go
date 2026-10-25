package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/config"
)

func TestAppSettingsDefaultOn(t *testing.T) {
	c := config.Default()
	if !c.App.Tray || !c.App.LaunchAtLogin {
		t.Fatalf("app settings default off: %+v", c.App)
	}
}

func TestSetCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "driga", "config.toml")
	if err := config.Set(path, map[string]any{"app.tray": false, "ui.theme": "dark"}); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.App.Tray || !c.App.LaunchAtLogin || c.UI.Theme != "dark" {
		t.Fatalf("got %+v %+v", c.App, c.UI)
	}
}

func TestSetKeepsTheRestOfTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	original := "# my roots\nroots = ['~/code']\n\n[ui]\n# chosen by hand\ntheme = 'light'\n\n[inbox]\nalerts = 'all'\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	if err := config.Set(path, map[string]any{"ui.theme": "system", "app.launch_at_login": false}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, keep := range []string{"# my roots\n", "roots = ['~/code']\n", "# chosen by hand\n", "[inbox]\nalerts = 'all'\n"} {
		if !strings.Contains(text, keep) {
			t.Errorf("lost %q:\n%s", keep, text)
		}
	}
	if strings.Contains(text, "light") || strings.Count(text, "theme =") != 1 {
		t.Errorf("theme not replaced in place:\n%s", text)
	}
	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.UI.Theme != "system" || c.App.LaunchAtLogin || !c.App.Tray || c.Inbox.Alerts != "all" || len(c.Roots) != 1 {
		t.Fatalf("got %+v", c)
	}
	// Setting it again edits the section added the first time.
	if err := config.Set(path, map[string]any{"app.launch_at_login": true}); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if strings.Count(string(b), "[app]") != 1 || strings.Count(string(b), "launch_at_login") != 1 {
		t.Fatalf("section duplicated:\n%s", b)
	}
}

func TestSetRefusesInvalidValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	original := "[ui]\ntheme = 'dark'\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	for _, values := range []map[string]any{
		{"ui.theme": "mocha"},
		{"app.tray": "yes"},
		{"app.unknown": true},
		{"roots": []string{"x"}},
	} {
		if err := config.Set(path, values); err == nil {
			t.Errorf("%v accepted", values)
		}
	}
	if b, _ := os.ReadFile(path); string(b) != original {
		t.Fatalf("file changed after a refused set:\n%s", b)
	}
}
