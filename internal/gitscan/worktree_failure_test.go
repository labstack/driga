package gitscan

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestScanRetiresMissingMarkersAndRetriesCorruption(t *testing.T) {
	gitEnv(t)
	repo := initRepo(t, filepath.Join(t.TempDir(), "repo"))
	linked := filepath.Join(t.TempDir(), "linked")
	gitTest(t, repo, "worktree", "add", "-b", "topic", linked)
	linked = NormalizePath(linked)
	scanner := NewScanner(2)
	if _, err := scanner.Scan(context.Background(), repo, Full); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(linked, ".git")
	original, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	var polls atomic.Int32
	run := scanner.runGit
	scanner.runGit = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		if dir == linked {
			polls.Add(1)
		}
		return run(ctx, dir, args...)
	}
	for _, mode := range []ScanMode{Cached, Full, Identity} {
		ws, err := scanner.Scan(context.Background(), repo, mode)
		if err != nil || len(ws) != 1 || ws[0].Path != repo {
			t.Fatalf("retired inventory: %+v %v", ws, err)
		}
	}
	if polls.Load() != 0 {
		t.Fatalf("retired checkout polled %d times", polls.Load())
	}
	if err := os.WriteFile(marker, []byte("gitdir: missing\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		ws, err := scanner.Scan(context.Background(), repo, Cached)
		if err != nil || len(ws) != 2 {
			t.Fatalf("partial scan: %+v %v", ws, err)
		}
		for _, w := range ws {
			if w.Path == linked && w.ScanError == "" {
				t.Fatalf("missing diagnostic: %+v", w)
			}
			if w.Path == repo && (w.ScanError != "" || w.Head == "") {
				t.Fatalf("healthy checkout: %+v", w)
			}
		}
	}
	if polls.Load() != 2 {
		t.Fatalf("failed checkout was not retried: %d", polls.Load())
	}
	if err := os.WriteFile(marker, original, 0600); err != nil {
		t.Fatal(err)
	}
	ws, err := scanner.Scan(context.Background(), repo, Cached)
	if err != nil || len(ws) != 2 {
		t.Fatalf("recovery: %+v %v", ws, err)
	}
	for _, w := range ws {
		if w.ScanError != "" || w.LastCommitSubject == "" {
			t.Fatalf("failed recovery: %+v", w)
		}
	}
}
