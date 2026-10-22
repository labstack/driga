package hooks_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/hooks"
)

func TestInstallRepairsMovedBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell and symlink fixtures")
	}
	for _, agent := range []string{"claude", "codex", "copilot"} {
		t.Run(agent, func(t *testing.T) {
			dir := t.TempDir()
			old := filepath.Join(dir, "old bin", "driga")
			current := filepath.Join(dir, "new bin", "driga")
			if err := os.MkdirAll(filepath.Dir(current), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(current, []byte("binary"), 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "settings")
			install := hooks.ClaudeInstall
			before := `{"theme":"dark","hooks":{"Stop":[{"hooks":[{"command":"other-tool stop"}]}]}}`
			switch agent {
			case "codex":
				install = hooks.CodexInstall
				before = "# user settings\nnotify = ['other-tool', 'notify']\nmodel = 'test'\n"
			case "copilot":
				install = hooks.CopilotInstall
				before = ""
			}
			if before != "" {
				writeFixture(t, path, before)
			}
			if _, err := install(path, old, false, testNow); err != nil {
				t.Fatal(err)
			}
			original := readFixture(t, path)
			for _, liveOld := range []bool{false, true} {
				if liveOld {
					if err := os.MkdirAll(filepath.Dir(old), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(old, []byte("old binary"), 0700); err != nil {
						t.Fatal(err)
					}
				}
				preview, err := install(path, current, true, testNow.Add(time.Second))
				if err != nil || !preview.Changed || !strings.Contains(preview.Unified(), current) || strings.Contains(preview.After, old) {
					t.Fatalf("repair: %+v, %v", preview, err)
				}
				if readFixture(t, path) != original {
					t.Fatal("preview wrote settings")
				}
			}
			fixed, err := install(path, current, false, testNow.Add(time.Second))
			if err != nil {
				t.Fatal(err)
			}
			if agent != "copilot" && !strings.Contains(fixed.After, "other-tool") {
				t.Fatal("foreign hook lost")
			}
			again, err := install(path, current, false, testNow.Add(2*time.Second))
			if err != nil || again.Changed {
				t.Fatalf("not idempotent: %+v %v", again, err)
			}
			alias := filepath.Join(dir, "driga")
			if err := os.Symlink(current, alias); err != nil {
				t.Fatal(err)
			}
			same, err := install(path, alias, true, testNow)
			if err != nil || same.Changed {
				t.Fatalf("same binary: %+v %v", same, err)
			}
			if agent != "copilot" {
				backups, _ := filepath.Glob(path + ".driga-bak-*")
				found := false
				for _, backup := range backups {
					found = found || readFixture(t, backup) == original
				}
				if !found {
					t.Fatal("repair backup missing")
				}
			}
		})
	}
}

func TestCodexRepairOwnedNotify(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell fixture")
	}
	for _, legacy := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "config.toml")
		before := "# keep this comment\n'notify' = [\n  '/old/driga', # executable\n  'hook', 'codex', 'notify'\n]\nmodel = 'fixture'\n"
		writeFixture(t, path, before)
		if !legacy {
			if _, err := hooks.CodexInstall(path, "/old/driga", false, testNow); err != nil {
				t.Fatal(err)
			}
		}
		diff, err := hooks.CodexInstall(path, "/new/driga", true, testNow)
		if err != nil || !diff.Changed || strings.Contains(diff.After, "/old/driga") || !strings.Contains(diff.After, "# executable") {
			t.Fatalf("repair notify: %+v %v", diff, err)
		}
	}
}

func TestHookStatusSameFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix symlink fixture")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, link := range []string{"symlink", "hardlink"} {
		dir := t.TempDir()
		alias := filepath.Join(dir, "driga")
		if link == "symlink" {
			err = os.Symlink(exe, alias)
		} else {
			err = os.Link(exe, alias)
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, agent := range []string{"claude", "codex", "copilot"} {
			path := filepath.Join(dir, agent)
			install := hooks.ClaudeInstall
			switch agent {
			case "codex":
				install = hooks.CodexInstall
			case "copilot":
				install = hooks.CopilotInstall
			}
			if _, err := install(path, alias, false, testNow); err != nil {
				t.Fatal(err)
			}
			diff, err := install(path, exe, true, testNow)
			if err != nil || diff.Changed {
				t.Fatalf("%s %s: changed=%v err=%v", agent, link, diff.Changed, err)
			}
			switch agent {
			case "claude":
				stale, err := hooks.ClaudeStale(path, exe)
				if err != nil || stale {
					t.Fatal(stale, err)
				}
			case "codex":
				status, err := hooks.CodexStatus(path, exe)
				if err != nil || status != "PermissionRequest installed" {
					t.Fatal(status, err)
				}
			case "copilot":
				status, err := hooks.CopilotStatus(path, exe)
				if err != nil || status != "installed" {
					t.Fatal(status, err)
				}
			}
		}
	}
}

func TestCodexRepairRetainsUninstallRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell fixture")
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	before := "# original\nmodel = 'fixture'\n"
	writeFixture(t, path, before)
	if _, err := hooks.CodexInstall(path, "/old/driga", false, testNow); err != nil {
		t.Fatal(err)
	}
	if _, err := hooks.CodexInstall(path, "/new/driga", false, testNow.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	diff, err := hooks.CodexUninstall(path, true)
	if err != nil || diff.After != before {
		t.Fatalf("round trip: %q %v", diff.After, err)
	}
}
