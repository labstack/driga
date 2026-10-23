package config_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/labstack/driga/internal/config"
)

func TestDefaults(t *testing.T) {
	c := config.Default()
	if c.RootDepth != 3 || c.StalledAfter != 10*time.Minute || c.OrphanMinAge != 10*time.Minute {
		t.Fatal(c)
	}
	if c.CaptureText != "titles" || len(c.Roots) != 0 {
		t.Fatal(c)
	}
	if !c.Adapters["claude"].Enabled || !c.Adapters["codex"].Enabled {
		t.Fatal(c)
	}
	if diff := cmp.Diff([]string{"**/node_modules", "**/.tmp/**"}, c.Exclude); diff != "" {
		t.Fatal(diff)
	}
	if c.NoServiceHint {
		t.Fatal("no_service_hint must default false")
	}
}

func TestLoadMissingFileIsDefault(t *testing.T) {
	c, err := config.Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(config.Default(), c); diff != "" {
		t.Fatal(diff)
	}
}

func TestLoadOverrides(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	p := filepath.Join(t.TempDir(), "c.toml")
	body := "stalled_after = \"3m\"\nroots = [\"~/code\"]\ncapture_text = \"snippets\"\n[adapters.codex]\nenabled = false\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.StalledAfter != 3*time.Minute || c.CaptureText != "snippets" {
		t.Fatal(c)
	}
	if diff := cmp.Diff([]string{filepath.Join(home, "code")}, c.Roots); diff != "" {
		t.Fatal(diff)
	}
	if c.Adapters["codex"].Enabled || !c.Adapters["claude"].Enabled {
		t.Fatal(c.Adapters)
	}
	if c.OrphanMinAge != 10*time.Minute {
		t.Fatal("unset keys must keep defaults")
	}
}

func TestLoadRejectsBadCaptureText(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	if err := os.WriteFile(p, []byte(`capture_text = "all"`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(p)
	if err == nil || !strings.Contains(err.Error(), "capture_text") {
		t.Fatalf("want capture_text error, got %v", err)
	}
}

func TestPathsHonorEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix path or action contract; Windows phase 1 has dedicated watch/refusal tests")
	}

	t.Setenv("DRIGA_DATA_DIR", "/x/data")
	t.Setenv("DRIGA_SPOOL_DIR", "/x/spool")
	t.Setenv("DRIGA_CONFIG", "/x/c.toml")
	if config.DataDir() != "/x/data" || config.SpoolDir() != "/x/spool" || config.Path() != "/x/c.toml" {
		t.Fatal(config.DataDir(), config.SpoolDir(), config.Path())
	}
	t.Setenv("DRIGA_DATA_DIR", "")
	t.Setenv("DRIGA_SPOOL_DIR", "")
	t.Setenv("DRIGA_CONFIG", "")
	t.Setenv("HOME", "/h")
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	wantData := "/h/Library/Application Support/driga"
	if runtime.GOOS == "linux" {
		t.Setenv("XDG_DATA_HOME", "")
		wantData = "/h/.local/share/driga"
	}
	wantSpool := wantData + "/events"
	if runtime.GOOS == "linux" {
		wantSpool = "/h/.local/state/driga/events"
	}
	if config.DataDir() != wantData || config.SpoolDir() != wantSpool || config.Path() != "/h/.config/driga/config.toml" {
		t.Fatal(config.DataDir(), config.SpoolDir(), config.Path())
	}
}

func TestConfigFleetDefaults(t *testing.T) {
	c := config.Default()
	if c.Fleet.Serve || c.Fleet.Listen != "" || c.Fleet.ShareText != "titles" || c.Fleet.EnrollWindow != 10*time.Minute || c.Fleet.AllowAnyAddress {
		t.Fatal(c.Fleet)
	}
	if c.Intake.OTLP || c.Intake.Listen != "127.0.0.1:7418" {
		t.Fatal(c.Intake)
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	body := `[fleet]
serve = true
listen = "127.0.0.1:0"
share_text = "snippets"
enroll_window = "3m"
allow_any_address = true
[intake]
otlp = true
listen = "[::1]:0"
`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Fleet.Serve || c.Fleet.Listen != "127.0.0.1:0" || c.Fleet.ShareText != "snippets" || c.Fleet.EnrollWindow != 3*time.Minute || !c.Fleet.AllowAnyAddress {
		t.Fatal(c.Fleet)
	}
	if !c.Intake.OTLP || c.Intake.Listen != "[::1]:0" {
		t.Fatal(c.Intake)
	}
}

func TestConfigFleetValidation(t *testing.T) {
	for _, tc := range []struct{ body, field string }{
		{"[fleet]\nshare_text = \"all\"", "share_text"},
		{"[fleet]\nenroll_window = \"0s\"", "enroll_window"},
		{"[fleet]\nenroll_window = \"-1m\"", "enroll_window"},
		{"[fleet]\nenroll_window = \"bad\"", "enroll_window"},
		{"[fleet]\nserve = true", "listen"},
		{"[intake]\nlisten = \"0.0.0.0:7418\"", "listen"},
		{"[intake]\nlisten = \"192.168.1.2:7418\"", "listen"},
		{"[intake]\nlisten = \"localhost:7418\"", "listen"},
		{"[intake]\nlisten = \"127.0.0.1:65536\"", "listen"},
	} {
		t.Run(tc.body, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(tc.body), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := config.Load(path); err == nil || !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("want %s error, got %v", tc.field, err)
			}
		})
	}
}

func TestCaptureNoneRejected(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(`capture_text = "none"`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(p); err == nil {
		t.Fatal("capture_text=none accepted")
	}
}

func TestMalformedExcludeSkippedAtLoad(t *testing.T) {
	for _, pattern := range []string{"[", "foo/[abc", "**/[abc"} {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte("exclude = ['good/**', '"+pattern+"', '**/node_modules']\n"), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(cfg.Exclude) != 2 || cfg.Exclude[0] != "good/**" || cfg.Exclude[1] != "**/node_modules" {
			t.Fatal(cfg.Exclude)
		}
		if len(cfg.Warnings) != 1 || !strings.Contains(cfg.Warnings[0], "exclude") || !strings.Contains(cfg.Warnings[0], pattern) {
			t.Fatal(cfg.Warnings)
		}
	}
}

// The Chat MCP child inherits these from its broker; config must accept them.
func TestChatChildEnvironmentAccepted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DRIGA_CONFIG", path)
	for _, name := range []string{"DRIGA_CHAT_SOCKET", "DRIGA_CHAT_CONVERSATION", "DRIGA_CHAT_TOKEN", "DRIGA_CHAT_READ_ONLY"} {
		t.Setenv(name, "x")
	}
	if _, err := config.Load(config.Path()); err != nil {
		t.Fatal(err)
	}
}
