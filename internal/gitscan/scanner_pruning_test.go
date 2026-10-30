package gitscan

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScannerPrunesRemovedRepositoriesAndWorktrees(t *testing.T) {
	s := NewScanner(1)
	base := t.TempDir()
	s.runGit = func(_ context.Context, dir string, args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "worktree list") {
			return []byte("worktree " + dir + "\nHEAD abc\nbranch refs/heads/main\n\nworktree " + filepath.Join(dir, "linked") + "\nHEAD abc\nbranch refs/heads/linked\n"), nil
		}
		if strings.Contains(strings.Join(args, " "), "status") {
			return []byte("# branch.head main\n"), nil
		}
		return []byte("123\x00subject\n"), nil
	}
	var paths []string
	for i := 0; i < 32; i++ {
		p := filepath.Join(base, fmt.Sprint(i))
		if err := os.MkdirAll(filepath.Join(p, "linked", ".git"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(p, ".git"), 0700); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
		if _, err := s.Scan(context.Background(), p, Full); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.locks) != 0 {
		t.Fatalf("retained %d idle locks", len(s.locks))
	}
	if err := os.RemoveAll(filepath.Join(paths[0], "linked")); err != nil {
		t.Fatal(err)
	}
	s.Prune(paths[:1])
	if len(s.repos) != 1 || len(s.cache) != 1 {
		t.Fatalf("repos=%d trees=%d", len(s.repos), len(s.cache))
	}
	s.Prune(nil)
	if len(s.repos) != 0 || len(s.cache) != 0 {
		t.Fatal("removed inventory retained")
	}
}
