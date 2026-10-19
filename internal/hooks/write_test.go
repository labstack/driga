package hooks

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteDiffRejectsChangedSettings(t *testing.T) {
	for _, name := range []string{"settings.json", "config.toml"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			if err := os.WriteFile(path, []byte("preview"), 0640); err != nil {
				t.Fatal(err)
			}
			d, err := readDiff(path)
			if err != nil {
				t.Fatal(err)
			}
			d.After, d.Changed = "installed", true
			if err := os.WriteFile(path, []byte("concurrent edit"), 0640); err != nil {
				t.Fatal(err)
			}
			err = writeDiff(d, false, time.Now())
			if err == nil || !strings.Contains(err.Error(), "settings changed since preview; rerun") {
				t.Fatalf("expected changed-settings refusal, got %v", err)
			}
			b, _ := os.ReadFile(path)
			if string(b) != "concurrent edit" {
				t.Fatalf("overwritten: %q", b)
			}
			entries, _ := os.ReadDir(filepath.Dir(path))
			if len(entries) != 1 {
				t.Fatalf("wrote files: %v", entries)
			}
		})
	}
}

func TestWriteDiffAtomicModeAndSymlink(t *testing.T) {
	for _, link := range []bool{false, true} {
		t.Run(map[bool]string{false: "regular", true: "symlink"}[link], func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "actual")
			if err := os.WriteFile(target, []byte("original"), 0640); err != nil {
				t.Fatal(err)
			}
			path := target
			if link {
				path = filepath.Join(dir, "settings")
				if err := os.Symlink("actual", path); err != nil {
					t.Fatal(err)
				}
			}
			d, err := readDiff(path)
			if err != nil {
				t.Fatal(err)
			}
			d.After, d.Changed = "new", true
			originalRename := renameSettings
			t.Cleanup(func() { renameSettings = originalRename })
			failure := errors.New("simulated pre-rename failure")
			renameSettings = func(_, _ string) error { return failure }
			if err := writeDiff(d, false, time.Now()); !errors.Is(err, failure) {
				t.Fatalf("expected simulated failure, got %v", err)
			}
			b, _ := os.ReadFile(target)
			if string(b) != "original" {
				t.Fatalf("failure changed original: %q", b)
			}
			renameSettings = originalRename
			if err := writeDiff(d, false, time.Now()); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(target)
			if err != nil || info.Mode().Perm() != 0640 {
				t.Fatalf("mode: %v %v", info, err)
			}
			b, _ = os.ReadFile(target)
			if string(b) != "new" {
				t.Fatalf("target: %q", b)
			}
			info, err = os.Lstat(path)
			if err != nil || (info.Mode()&os.ModeSymlink != 0) != link {
				t.Fatalf("symlink: %v %v", info, err)
			}
			temps, _ := filepath.Glob(filepath.Join(dir, ".driga-settings-*"))
			if len(temps) != 0 {
				t.Fatalf("leaked temps: %v", temps)
			}
		})
	}
}

func TestWriteDiffRefusesRetargetedLink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings")
	for _, name := range []string{"first", "second"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("original"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("first", path); err != nil {
		t.Fatal(err)
	}
	d, err := readDiff(path)
	if err != nil {
		t.Fatal(err)
	}
	d.After, d.Changed = "new", true
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("second", path); err != nil {
		t.Fatal(err)
	}
	err = d.Apply(time.Now())
	if err == nil || !strings.Contains(err.Error(), "settings changed since preview; rerun") {
		t.Fatalf("retarget accepted: %v", err)
	}
	for _, name := range []string{"first", "second"} {
		b, _ := os.ReadFile(filepath.Join(dir, name))
		if string(b) != "original" {
			t.Fatalf("%s overwritten", name)
		}
	}
}
