package config_test

import (
	"os"
	"path/filepath"
	"runtime"
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

func TestSetKeepsInlineComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	original := "motion = 'on'   # the Flow map moves\n\n[ui]\ntheme = \"a # b\" # picked by hand\n[app]\ntray=true#keep\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	if err := config.Set(path, map[string]any{"motion": "off", "ui.theme": "dark", "app.tray": false}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "motion = \"off\"   # the Flow map moves\n\n[ui]\ntheme = \"dark\" # picked by hand\n[app]\ntray = false#keep\n"
	if string(b) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", b, want)
	}
}

func TestSetWritesThroughASymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles", "driga.toml")
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("[ui]\ntheme = 'light'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "config", "config.toml")
	if err := os.MkdirAll(filepath.Dir(link), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../dotfiles/driga.toml", link); err != nil {
		t.Fatal(err)
	}
	if err := config.Set(link, map[string]any{"ui.theme": "dark"}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("config.toml is no longer a symlink: %v", err)
	}
	b, err := os.ReadFile(target)
	if err != nil || !strings.Contains(string(b), "theme = \"dark\"") {
		t.Fatalf("target not written: %q %v", b, err)
	}
}
