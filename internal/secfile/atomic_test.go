package secfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteChecksBeforeReplacingAndCleansTemps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings")
	if err := WritePrivate(path, []byte("original")); err != nil {
		t.Fatal(err)
	}
	refusal := errors.New("concurrent edit")
	err := WriteFileAtomic(path, []byte("replacement"), func() error {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 2 {
			t.Fatalf("temporary file missing: %v %v", entries, err)
		}
		for _, entry := range entries {
			if err := CheckPrivate(filepath.Join(dir, entry.Name())); err != nil {
				t.Fatal(err)
			}
		}
		b, _ := os.ReadFile(path)
		if string(b) != "original" {
			t.Fatalf("replaced before check: %q", b)
		}
		return refusal
	})
	if !errors.Is(err, refusal) {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("leaked temporary file: %v", entries)
	}
	if err := WriteFileAtomic(path, []byte("replacement"), nil); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "replacement" {
		t.Fatalf("replacement: %q", b)
	}
	if err := CheckPrivate(path); err != nil {
		t.Fatal(err)
	}
}

func TestAtomicWriteRenameFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "directory")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(path, []byte("data"), nil); err == nil {
		t.Fatal("replaced directory with file")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || !entries[0].IsDir() {
		t.Fatalf("failed rename changed directory or leaked temp: %v", entries)
	}
}
