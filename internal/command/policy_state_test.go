package command

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Commands run for an agent never reach any Driga build's state: release,
// development and driga-<name> variants, by default and through overrides,
// including through a symlink or a relative path.
func TestPolicyDeniesEveryDrigaVariantsState(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix home layout")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME"} {
		t.Setenv(key, "")
	}
	t.Setenv("DRIGA_DATA_DIR", filepath.Join(root, "override-data"))
	t.Setenv("DRIGA_SPOOL_DIR", filepath.Join(root, "override-spool"))
	t.Setenv("DRIGA_CONFIG", filepath.Join(root, "override.toml"))
	variant := filepath.Join(home, "Library", "Application Support", "driga-feature")
	linuxVariant := filepath.Join(home, ".local", "share", "driga-feature")
	for _, dir := range []string{variant, linuxVariant} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(project, "link")
	if err := os.Symlink(filepath.Join(home, "Library", "Application Support", "driga-dev"), link); err != nil {
		t.Fatal(err)
	}
	s, err := normalizeScope(Scope{Project: project})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(home, "Library", "Application Support", "driga", "driga.db"),
		filepath.Join(home, "Library", "Application Support", "driga-dev", "driga.db"),
		filepath.Join(home, "Library", "Application Support", "driga-dev", "events", "20261102.jsonl"),
		filepath.Join(home, "Library", "Logs", "driga-dev", "service.log"),
		filepath.Join(variant, "driga.db"),
		filepath.Join(home, ".local", "share", "driga", "driga.db"),
		filepath.Join(home, ".local", "share", "driga-dev", "driga.db"),
		filepath.Join(home, ".local", "state", "driga-dev", "events", "20261102.jsonl"),
		filepath.Join(linuxVariant, "driga.db"),
		filepath.Join(home, ".config", "driga", "config.toml"),
		filepath.Join(root, "override-data", "driga.db"),
		filepath.Join(root, "override-spool", "20261102.jsonl"),
		filepath.Join(root, "override.toml"),
		filepath.Join(link, "driga.db"),
		filepath.Join(project, "..", "home", ".local", "share", "driga-dev", "fleet.key"),
	} {
		canon, err := canonical(path)
		if err != nil {
			t.Fatal(err)
		}
		if !denied(canon, s) {
			t.Errorf("%s (%s) is not denied", path, canon)
		}
	}
	if denied(filepath.Join(project, "notes.txt"), s) || denied(filepath.Join(root, "override-data-other"), s) {
		t.Error("the project or a sibling of an override is denied")
	}
}

// canonical resolves symlinks as the kernel does, so no chain of links
// reaches a denied directory under another name.
func TestCanonicalFollowsSymlinksLikeTheKernel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix symlinks")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	denied := filepath.Join(root, "home", ".local", "share", "driga-dev")
	deep := filepath.Join(root, "real", "a")
	for _, dir := range []string{denied, deep} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	symlink := func(target, link string) {
		t.Helper()
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	// A relative link inside a symlinked parent resolves against the
	// parent's target, not against the alias.
	symlink("../../home/.local/share/driga-dev", filepath.Join(deep, "link"))
	symlink(deep, filepath.Join(root, "alias"))
	// An absolute link, and a dangling one into a directory not yet made.
	symlink(denied, filepath.Join(root, "absolute"))
	symlink(filepath.Join(root, "home", ".local", "share", "driga-feature", "new"), filepath.Join(root, "dangling"))
	// filepath.Join would clean the ".." lexically; the kernel does not.
	// A loop.
	symlink(filepath.Join(root, "loop-b"), filepath.Join(root, "loop-a"))
	symlink(filepath.Join(root, "loop-a"), filepath.Join(root, "loop-b"))
	for in, want := range map[string]string{
		filepath.Join(root, "alias", "link", "driga.db"):     filepath.Join(denied, "driga.db"),
		filepath.Join(root, "absolute", "events", "x.jsonl"): filepath.Join(denied, "events", "x.jsonl"),
		filepath.Join(root, "dangling", "missing", "tail"):   filepath.Join(root, "home", ".local", "share", "driga-feature", "new", "missing", "tail"),
		root + "/alias/../a/link/../driga-dev":               filepath.Join(root, "home", ".local", "share", "driga-dev"),
		root + "/real/missing/../a":                          filepath.Join(root, "real", "a"),
	} {
		got, err := canonical(in)
		if err != nil || got != want {
			t.Errorf("canonical(%s) = %s, %v; want %s", in, got, err, want)
		}
	}
	if _, err := canonical(filepath.Join(root, "loop-a", "x")); err == nil {
		t.Error("a symlink loop resolved")
	}
}

// Symlink targets are rooted by the platform's rules, whatever the host,
// and a Windows target this walk does not model fails closed.
func TestLinkTargetRooting(t *testing.T) {
	for _, tc := range []struct {
		windows          bool
		dir, target      string
		base, components string
	}{
		{false, "/home/a", "../b", "/home/a", "../b"},
		{false, "/home/a", "/var/driga", "/", "var/driga"},
		{true, `C:\Users\a`, `..\b`, `C:\Users\a`, "../b"},
		{true, `C:\Users\a`, `\Users\x\AppData\Local\driga`, `C:\`, "Users/x/AppData/Local/driga"},
		{true, `D:\work`, `/Users/x/AppData/Local/driga-dev`, `D:\`, "Users/x/AppData/Local/driga-dev"},
		{true, `C:\Users\a`, `D:\driga`, `D:\`, "driga"},
	} {
		base, parts, err := linkTarget(tc.windows, tc.dir, tc.target)
		if err != nil || base != tc.base || strings.Join(parts, "/") != tc.components {
			t.Errorf("linkTarget(%t, %q, %q) = %q, %q, %v; want %q, %q", tc.windows, tc.dir, tc.target, base, parts, err, tc.base, tc.components)
		}
	}
	for _, target := range []string{
		`C:driga`, `\\server\share\driga`, `\\?\C:\Users\x\driga`, `\\?\UNC\server\share\driga`,
		`\\?\Volume{0b1c2d3e-0000-0000-0000-000000000000}\driga`, `\\?\GLOBALROOT\Device\HarddiskVolume1\driga`,
		`\\.\C:\driga`, `\\.\pipe\driga`, `//server/share/driga`, `driga:stream`,
	} {
		if _, _, err := linkTarget(true, `C:\Users\a`, target); !errors.Is(err, errUnresolvableLink) {
			t.Errorf("linkTarget(%q) = %v; want it to fail closed", target, err)
		}
	}
	if _, _, err := linkTarget(true, `\\server\share\a`, `\driga`); !errors.Is(err, errUnresolvableLink) {
		t.Errorf("a rooted target on a UNC share = %v; want it to fail closed", err)
	}
}
