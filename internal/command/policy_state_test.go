package command

import (
	"os"
	"path/filepath"
	"runtime"
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
