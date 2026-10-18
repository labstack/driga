package gitscan

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func gitEnv(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR"} {
		t.Setenv(key, "") // Register restoration, then remove Git's path override.
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
}

func gitTest(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return out
}

func writeTest(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func initRepo(t *testing.T, p string) string {
	t.Helper()
	if err := os.MkdirAll(p, 0700); err != nil {
		t.Fatal(err)
	}
	gitTest(t, p, "init", "-b", "main")
	gitTest(t, p, "config", "user.name", "Test User")
	gitTest(t, p, "config", "user.email", "test@example.invalid")
	writeTest(t, filepath.Join(p, "file"), "initial\n")
	gitTest(t, p, "add", "file")
	gitTest(t, p, "commit", "-m", "initial subject ü")
	return NormalizePath(p)
}

func scanOne(t *testing.T, s *Scanner, p string, full bool) model.Worktree {
	t.Helper()
	w, err := s.Scan(context.Background(), p, full)
	if err != nil || len(w) != 1 {
		t.Fatalf("scan: %#v, %v", w, err)
	}
	return w[0]
}

func TestScanDirtyAndCommit(t *testing.T) {
	gitEnv(t)
	p := initRepo(t, filepath.Join(t.TempDir(), "repo"))
	writeTest(t, filepath.Join(p, "file"), "modified\n")
	old := time.Now().Add(-8 * 24 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(filepath.Join(p, "file"), old, old); err != nil {
		t.Fatal(err)
	}
	w := scanOne(t, NewScanner(4), p, true)
	if w.Dirty != 1 || w.LastCommitSubject != "initial subject ü" || w.LastCommitAt.IsZero() || w.UpdatedAt.IsZero() || w.Head == "" || w.Branch != "main" || w.Path != p || !w.OldestDirtyMtime.Equal(old) {
		t.Fatal(w)
	}
}

func TestScanSkipsUnchangedUnlessFull(t *testing.T) {
	gitEnv(t)
	p := initRepo(t, filepath.Join(t.TempDir(), "repo"))
	s := NewScanner(2)
	original := s.runGit
	var calls int
	s.runGit = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		calls++
		return original(ctx, dir, args...)
	}
	first := scanOne(t, s, p, false)
	calls = 0
	second := scanOne(t, s, p, false)
	if calls != 0 || !reflect.DeepEqual(first, second) {
		t.Fatalf("calls=%d first=%#v second=%#v", calls, first, second)
	}
	scanOne(t, s, p, true)
	if calls == 0 {
		t.Fatal("full scan skipped git")
	}
	index := filepath.Join(p, ".git", "index")
	future := time.Now().Add(time.Minute)
	if err := os.Chtimes(index, future, future); err != nil {
		t.Fatal(err)
	}
	calls = 0
	scanOne(t, s, p, false)
	if calls == 0 {
		t.Fatal("changed index skipped git")
	}
}

func TestScanDetectsWorkingFileEditOnFull(t *testing.T) {
	gitEnv(t)
	p := initRepo(t, filepath.Join(t.TempDir(), "repo"))
	s := NewScanner(1)
	scanOne(t, s, p, false)
	writeTest(t, filepath.Join(p, "file"), "working edit\n")
	if w := scanOne(t, s, p, false); w.Dirty != 0 {
		t.Fatal(w)
	}
	if w := scanOne(t, s, p, true); w.Dirty != 1 {
		t.Fatal(w)
	}
}

func TestDiscoverReposNormalizesPaths(t *testing.T) {
	gitEnv(t)
	base := t.TempDir()
	p := initRepo(t, filepath.Join(base, "repo space ü"))
	link := filepath.Join(base, "alias")
	if err := os.Symlink(p, link); err != nil {
		t.Fatal(err)
	}
	got, err := DiscoverRepos([]string{p + "/", link, filepath.Join(link, "missing", "child")}, []string{base}, 2, nil)
	if err != nil || !reflect.DeepEqual(got, []string{p}) {
		t.Fatalf("got %v, %v", got, err)
	}
	if got := NormalizePath(filepath.Join(link, "missing", "child")); got != filepath.Join(p, "missing", "child") {
		t.Fatal(got)
	}
	t.Run("tmp alias", func(t *testing.T) {
		if _, err := os.Stat("/private/tmp"); err != nil {
			t.Skip("/private/tmp unavailable")
		}
		t.Setenv("TMPDIR", "/tmp")
		tmp, err := os.MkdirTemp(os.TempDir(), "driga-gitscan-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(tmp) })
		repo := initRepo(t, filepath.Join(tmp, "repo ü"))
		if !strings.HasPrefix(repo, "/private/tmp/") && !strings.HasPrefix(repo, "/tmp/") {
			t.Skip("os.TempDir is not /tmp")
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(repo, "/private"), "/tmp/")
		got, err := DiscoverRepos([]string{filepath.Join("/tmp", rel), filepath.Join("/private/tmp", rel)}, nil, 0, nil)
		if err != nil || !reflect.DeepEqual(got, []string{repo}) {
			t.Fatalf("%v, %v", got, err)
		}
	})
}

func TestDiscoverReposExclude(t *testing.T) {
	gitEnv(t)
	base := t.TempDir()
	good := initRepo(t, filepath.Join(base, "good"))
	initRepo(t, filepath.Join(base, "node_modules", "hidden"))
	initRepo(t, filepath.Join(base, ".tmp", "hidden"))
	initRepo(t, filepath.Join(base, "deep", "hidden"))
	initRepo(t, filepath.Join(good, "nested"))
	got, err := DiscoverRepos(nil, []string{base}, 1, []string{"**/node_modules", "**/.tmp/**"})
	if err != nil || !reflect.DeepEqual(got, []string{good}) {
		t.Fatalf("%v, %v", got, err)
	}
	got, err = DiscoverRepos(nil, []string{base}, 4, []string{"**/node_modules", "**/.tmp/**", "**/deep/**"})
	if err != nil || !reflect.DeepEqual(got, []string{good}) {
		t.Fatalf("%v, %v", got, err)
	}
}

func TestLinkedWorktreeGitFile(t *testing.T) {
	gitEnv(t)
	base := t.TempDir()
	p := initRepo(t, filepath.Join(base, "main repo"))
	linked := filepath.Join(base, "linked ü")
	gitTest(t, p, "worktree", "add", "-b", "feat/x", linked)
	got, err := DiscoverRepos([]string{linked}, []string{linked}, 1, nil)
	if err != nil || !reflect.DeepEqual(got, []string{p}) {
		t.Fatalf("%v, %v", got, err)
	}
	s := NewScanner(2)
	w, err := s.Scan(context.Background(), p, true)
	if err != nil || len(w) != 2 {
		t.Fatalf("%v, %v", w, err)
	}
	gitTest(t, linked, "checkout", "--detach")
	w, err = s.Scan(context.Background(), p, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, wt := range w {
		if wt.Path == NormalizePath(linked) && wt.Branch != "(detached)" {
			t.Fatal(wt)
		}
	}
}

func TestScanUntrackedEveryFifthFull(t *testing.T) {
	gitEnv(t)
	p := initRepo(t, filepath.Join(t.TempDir(), "repo"))
	writeTest(t, filepath.Join(p, "untracked"), "new")
	s := NewScanner(1)
	for i := 1; i <= 5; i++ {
		w := scanOne(t, s, p, true)
		want := 0
		if i == 5 {
			want = 1
		}
		if w.Untracked != want || w.Dirty != 0 {
			t.Fatalf("pass %d: %#v", i, w)
		}
		scanOne(t, s, p, false)
	}
	if err := os.Remove(filepath.Join(p, "untracked")); err != nil {
		t.Fatal(err)
	}
	if w := scanOne(t, s, p, true); w.Untracked != 1 {
		t.Fatal(w)
	}
	for i := 7; i <= 10; i++ {
		scanOne(t, s, p, true)
	}
	if w := scanOne(t, s, p, false); w.Untracked != 0 {
		t.Fatal(w)
	}
}

func TestScanOldestDirtyRenameAndDeletion(t *testing.T) {
	gitEnv(t)
	p := initRepo(t, filepath.Join(t.TempDir(), "repo"))
	writeTest(t, filepath.Join(p, "deleted"), "delete me")
	gitTest(t, p, "add", "deleted")
	gitTest(t, p, "commit", "-m", "second")
	gitTest(t, p, "mv", "file", "renamed ü")
	if err := os.Remove(filepath.Join(p, "deleted")); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-9 * 24 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(filepath.Join(p, "renamed ü"), old, old); err != nil {
		t.Fatal(err)
	}
	w := scanOne(t, NewScanner(1), p, true)
	if w.Dirty != 2 || !w.OldestDirtyMtime.Equal(old) {
		t.Fatal(w)
	}
}

func TestScanDirtySymlinkMtime(t *testing.T) {
	gitEnv(t)
	p := initRepo(t, filepath.Join(t.TempDir(), "repo"))
	link := filepath.Join(p, "link")
	if err := os.Symlink("file", link); err != nil {
		t.Fatal(err)
	}
	gitTest(t, p, "add", "link")
	gitTest(t, p, "commit", "-m", "track link")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing-target", link); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	w := scanOne(t, NewScanner(1), p, true)
	if w.Dirty != 1 || !w.OldestDirtyMtime.Equal(info.ModTime()) {
		t.Fatalf("worktree=%#v, symlink mtime=%s", w, info.ModTime())
	}
}

func TestScanConcurrentBoundAndCancellation(t *testing.T) {
	s := NewScanner(2)
	var active, peak atomic.Int32
	s.runGit = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 10*time.Second {
			t.Error("missing command timeout")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
		if strings.Contains(strings.Join(args, " "), "worktree list") {
			return []byte("worktree " + dir + "\nHEAD abc\nbranch refs/heads/main\n"), nil
		}
		if strings.Contains(strings.Join(args, " "), "status") {
			return []byte("# branch.head main\n"), nil
		}
		return []byte("123\x00subject\n"), nil
	}
	base := t.TempDir()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.Scan(context.Background(), filepath.Join(base, string(rune('a'+i%4))), true)
			if err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if peak.Load() > 2 || peak.Load() < 2 {
		t.Fatalf("peak concurrency %d", peak.Load())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Scan(ctx, base, true); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestScanUnbornAndBare(t *testing.T) {
	gitEnv(t)
	p := t.TempDir()
	gitTest(t, p, "init", "-b", "main")
	w := scanOne(t, NewScanner(0), p, true)
	if w.Branch != "main" || !w.LastCommitAt.IsZero() || w.Head != "" {
		t.Fatal(w)
	}
	bare := filepath.Join(t.TempDir(), "bare")
	gitTest(t, "", "init", "--bare", bare)
	got, err := NewScanner(1).Scan(context.Background(), bare, true)
	if err != nil || len(got) != 0 {
		t.Fatalf("%v, %v", got, err)
	}
}

func TestScanSkipsMissingLinkedWorktree(t *testing.T) {
	gitEnv(t)
	base := t.TempDir()
	p := initRepo(t, filepath.Join(base, "main"))
	linked := filepath.Join(base, "gone")
	gitTest(t, p, "worktree", "add", "-b", "feat/gone", linked)
	if err := os.RemoveAll(linked); err != nil {
		t.Fatal(err)
	}
	w, err := NewScanner(2).Scan(context.Background(), p, true)
	if err != nil || len(w) != 1 || w[0].Path != NormalizePath(p) {
		t.Fatalf("want only main worktree, got %v, %v", w, err)
	}
}
