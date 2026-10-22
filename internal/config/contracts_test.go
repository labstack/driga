package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestStrictConfiguration(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	for _, tc := range []struct{ body, key string }{
		{"typo = true", "typo"}, {"[unknown]\n", "unknown"},
		{"[fleet]\nlitsen = 'x'", "fleet.litsen"},
		{"[adapters.claude]\nenabeld = true", "adapters.claude.enabeld"},
		{"[adapters.typo]\nenabled = true", "adapters.typo"},
		{"stuck_after = '1m'", "stuck_after"},
		{"stalled_after = '-1s'", "stalled_after"},
		{"orphan_min_age = '-1s'", "orphan_min_age"},
		{"[cleanup]\nlow_disk_bytes = '2GiB'", "cleanup.low_disk_bytes"},
	} {
		t.Run(tc.key+tc.body, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(p, []byte(tc.body), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(p)
			if err == nil || !strings.Contains(err.Error(), p) || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("wanted key and file, got %v", err)
			}
		})
	}
}

func TestEnvironmentConfiguration(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte("capture_text = 'titles'\n[summaries]\nenabled = false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DRIGA_CAPTURE_TEXT", "snippets")
	t.Setenv("DRIGA_SUMMARIES_ENABLED", "true")
	t.Setenv("DRIGA_CLEANUP_WORKTREE_IDLE", "0s")
	t.Setenv("DRIGA_FLEET_LISTEN", "127.0.0.1:0")
	t.Setenv("DRIGA_INTAKE_OTLP", "true")
	c, err := Load(p)
	if err != nil || c.CaptureText != "snippets" || !c.Summaries.Enabled || c.Cleanup.WorktreeIdle != 0 || c.Fleet.Listen != "127.0.0.1:0" || !c.Intake.OTLP {
		t.Fatal(c, err)
	}
	t.Setenv("DRIGA_CAPTURE_TEXT", "")
	c, err = Load(p)
	if err != nil || c.CaptureText != "titles" {
		t.Fatal(c, err)
	}
	t.Setenv("DRIGA_CLEANUP_WORKTREE_IDLE", "-1s")
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "DRIGA_CLEANUP_WORKTREE_IDLE") || !strings.Contains(err.Error(), "range") {
		t.Fatal(err)
	}
}

func TestUnknownEnvironmentEvenWithoutFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "missing")
	for _, name := range []string{"DRIGA_INTERNAL", "DRIGA_FIXTURE_OUTPUT", "DRIGA_COLLECTOR_HEAP_CHILD", "DRIGA_FLEET_HEAP_CHILD", "DRIGA_OTLP_TRACES_ADDED", "DRIGA_JSON_WATCH_CHILD", "DRIGA_TEST_MCP_PROCESS"} {
		t.Setenv(name, "1")
	}
	if _, err := Load(p); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DRIGA_SUMMARIES_ENABELD", "")
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "DRIGA_SUMMARIES_ENABELD") {
		t.Fatal(err)
	}
}

func TestConfigAndSpoolXDGPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix path or action contract; Windows phase 1 has dedicated watch/refusal tests")
	}

	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("DRIGA_CONFIG", "")
	t.Setenv("DRIGA_SPOOL_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, "config"))
	if got := Path(); got != filepath.Join(h, "config/driga/config.toml") {
		t.Fatal(got)
	}
	t.Setenv("XDG_CONFIG_HOME", "relative")
	if got := Path(); got != filepath.Join(h, ".config/driga/config.toml") {
		t.Fatal(got)
	}
	t.Setenv("XDG_STATE_HOME", filepath.Join(h, "state"))
	if got := spoolDirForOS("linux"); got != filepath.Join(h, "state/driga/events") {
		t.Fatal(got)
	}
	if got := spoolDirForOS("darwin"); got != filepath.Join(h, "Library/Application Support/driga/events") {
		t.Fatal(got)
	}
	t.Setenv("XDG_DATA_HOME", filepath.Join(h, "xdg-data"))
	t.Setenv("DRIGA_DATA_DIR", filepath.Join(h, "custom-data"))
	if got := spoolDirForOS("darwin"); got != filepath.Join(h, "Library/Application Support/driga/events") {
		t.Fatal("macOS spool must ignore data and XDG overrides", got)
	}
	t.Setenv("DRIGA_SPOOL_DIR", filepath.Join(h, "custom-spool"))
	for _, goos := range []string{"darwin", "linux", "windows"} {
		if got := spoolDirForOS(goos); got != filepath.Join(h, "custom-spool") {
			t.Fatal(goos, got)
		}
	}
	t.Setenv("DRIGA_SPOOL_DIR", "")
	t.Setenv("XDG_STATE_HOME", "relative")
	if got := spoolDirForOS("linux"); got != filepath.Join(h, ".local/state/driga/events") {
		t.Fatal(got)
	}
	if got := spoolDirForOS("darwin"); got != filepath.Join(h, "Library/Application Support/driga/events") {
		t.Fatal(got)
	}
}

func TestZeroDurations(t *testing.T) {
	t.Setenv("DRIGA_STALLED_AFTER", "0s")
	t.Setenv("DRIGA_ORPHAN_MIN_AGE", "0s")
	c, err := Load(filepath.Join(t.TempDir(), "missing"))
	if err != nil || c.StalledAfter != 10*time.Minute || c.OrphanMinAge != 0 {
		t.Fatal(c, err)
	}
	t.Setenv("DRIGA_FLEET_ENROLL_WINDOW", "0s")
	if _, err := Load(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("zero enrollment window accepted")
	}
}

func TestGeneratedEnvironmentExample(t *testing.T) {
	b, err := os.ReadFile("../../.env.example")
	if err != nil {
		t.Fatal(err)
	}
	if strings.ReplaceAll(string(b), "\r\n", "\n") != EnvExample() {
		t.Fatal(".env.example is stale; run go generate ./internal/config")
	}
	names := map[string]bool{}
	for _, s := range Settings() {
		if name := s.EnvName(); name != "" {
			if names[name] {
				t.Fatal("environment collision", name)
			}
			names[name] = true
		}
	}
	// Git checkouts can use CRLF. Validate both forms, including blank lines.
	lf := strings.ReplaceAll(string(b), "\r\n", "\n")
	for _, template := range []string{lf, strings.ReplaceAll(lf, "\n", "\r\n")} {
		for _, line := range strings.Split(template, "\n") {
			line = strings.TrimSuffix(line, "\r")
			if line != "" && !strings.HasPrefix(line, "#") {
				t.Fatal("active template assignment", line)
			}
		}
	}
}

func TestSourceProvenance(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte("capture_text = 'titles'\n[adapters.codex]\nenabled = false"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DRIGA_CAPTURE_TEXT", "snippets")
	t.Setenv("DRIGA_ADAPTERS_CODEX_ENABLED", "true")
	_, sources, err := LoadWithSources(p)
	if err != nil || sources["capture_text"] != "env" || sources["adapters.codex.enabled"] != "env" || sources["root_depth"] != "default" {
		t.Fatal(sources, err)
	}
	t.Setenv("DRIGA_CAPTURE_TEXT", "")
	_, sources, err = LoadWithSources(p)
	if err != nil || sources["capture_text"] != "file" {
		t.Fatal(sources, err)
	}
}

func TestFlagPrecedenceBeforeRelatedValidation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte("[fleet]\nserve = true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadWithOverrides(p, map[string]any{"fleet.listen": "127.0.0.1:0"})
	if err != nil || c.Fleet.Listen != "127.0.0.1:0" {
		t.Fatal(c, err)
	}
	t.Setenv("DRIGA_FLEET_LISTEN", "127.0.0.1:7411")
	c, err = LoadWithOverrides(p, map[string]any{"fleet.listen": "127.0.0.1:0"})
	if err != nil || c.Fleet.Listen != "127.0.0.1:0" {
		t.Fatal(c, err)
	}
}

func TestAllScalarEnvironmentMappings(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{"DRIGA_ROOT_DEPTH", "0"}, {"DRIGA_STALLED_AFTER", "2m"}, {"DRIGA_ORPHAN_MIN_AGE", "3m"},
		{"DRIGA_CAPTURE_TEXT", "snippets"}, {"DRIGA_OPEN_CMD", "code {cwd}"}, {"DRIGA_NO_SERVICE_HINT", "true"},
		{"DRIGA_ADAPTERS_CLAUDE_ENABLED", "false"}, {"DRIGA_ADAPTERS_CODEX_ENABLED", "false"}, {"DRIGA_ADAPTERS_GEMINI_ENABLED", "false"},
		{"DRIGA_SUMMARIES_ENABLED", "true"}, {"DRIGA_SUMMARIES_MODEL", "custom"},
		{"DRIGA_CLEANUP_WORKTREE_IDLE", "1h"},
		{"DRIGA_FLEET_SERVE", "false"}, {"DRIGA_FLEET_LISTEN", "127.0.0.1:7411"}, {"DRIGA_FLEET_SHARE_TEXT", "none"},
		{"DRIGA_FLEET_ENROLL_WINDOW", "1m"}, {"DRIGA_FLEET_ALLOW_ANY_ADDRESS", "true"},
		{"DRIGA_INTAKE_OTLP", "true"}, {"DRIGA_INTAKE_LISTEN", "[::1]:0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.name, tc.value)
			_, sources, err := LoadWithSources(filepath.Join(t.TempDir(), "missing"))
			if err != nil {
				t.Fatal(err)
			}
			key := strings.ToLower(strings.TrimPrefix(tc.name, "DRIGA_"))
			found := false
			for name, source := range sources {
				if strings.ReplaceAll(name, ".", "_") == key && source == "env" {
					found = true
				}
			}
			if !found {
				t.Fatal("missing mapping", tc.name)
			}
		})
	}
}

func TestInvalidEnvironmentRangesAreRedacted(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{"DRIGA_STALLED_AFTER", "-1ns"}, {"DRIGA_ORPHAN_MIN_AGE", "-1h"}, {"DRIGA_CLEANUP_WORKTREE_IDLE", "999999999999h"},
		{"DRIGA_MACHINE_DISK_FREE_MIN", "18446744073709551616"}, {"DRIGA_MACHINE_DISK_FREE_MIN", "-1GiB"},
		{"DRIGA_ROOT_DEPTH", "-1"}, {"DRIGA_CAPTURE_TEXT", "private-value"},
		{"DRIGA_FLEET_ENROLL_WINDOW", "11m"},
	} {
		t.Run(tc.name+tc.value, func(t *testing.T) {
			t.Setenv(tc.name, tc.value)
			_, err := Load(filepath.Join(t.TempDir(), "missing"))
			if err == nil || !strings.Contains(err.Error(), tc.name) || strings.Contains(err.Error(), "private-value") {
				t.Fatal(err)
			}
		})
	}
}
