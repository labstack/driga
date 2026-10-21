package gitscan

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/pathx"
	"github.com/labstack/driga/internal/platform"
)

// NormalizePath resolves absolute paths and symlinks, including existing parents
// of paths that have disappeared or have not yet been created.
func NormalizePath(p string) string {
	if foreignPath(p) {
		return p
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	p = filepath.Clean(abs)
	prefix := p
	var suffix []string
	for {
		if resolved, err := filepath.EvalSymlinks(prefix); err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return filepath.Clean(resolved)
		}
		parent := filepath.Dir(prefix)
		if parent == prefix {
			return p
		}
		suffix = append(suffix, filepath.Base(prefix))
		prefix = parent
	}
}

func foreignPath(p string) bool {
	return pathx.RecordedIsAbs(p) && pathx.WindowsStyle(p) != (runtime.GOOS == "windows")
}

// DiscoverRepos finds repositories above cwds and at most depth levels below
// roots. Linked worktrees resolve to their main repository, without running Git.
func DiscoverRepos(cwds, roots []string, depth int, exclude []string) ([]string, error) {
	found := make(map[string]bool)
	for _, cwd := range cwds {
		if cwd == "" || foreignPath(cwd) {
			continue
		}
		for p := NormalizePath(cwd); ; p = filepath.Dir(p) {
			repo, err := repoAt(p)
			if err != nil {
				return nil, err
			}
			if repo != "" {
				found[repo] = true
				break
			}
			if filepath.Dir(p) == p {
				break
			}
		}
	}
	var walk func(string, int) error
	walk = func(p string, level int) error {
		if filepath.Base(p) == ".git" || excluded(p, exclude) {
			return nil
		}
		info, err := os.Stat(p)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return nil
		}
		repo, err := repoAt(p)
		if err != nil {
			return err
		}
		if repo != "" {
			found[repo] = true
			return nil
		}
		if level >= depth {
			return nil
		}
		entries, err := os.ReadDir(p)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			// Do not follow directory symlinks while walking; explicit roots and
			// cwds are normalized above, and cycles must not expand discovery.
			if entry.IsDir() {
				if err := walk(filepath.Join(p, entry.Name()), level+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, root := range roots {
		if root != "" {
			if err := walk(NormalizePath(root), 0); err != nil {
				return nil, err
			}
		}
	}
	result := make([]string, 0, len(found))
	for p := range found {
		result = append(result, p)
	}
	sort.Strings(result)
	return result, nil
}

func excluded(p string, patterns []string) bool {
	parts := strings.Split(strings.Trim(filepath.ToSlash(p), "/"), "/")
	var match func([]string, []string) bool
	match = func(pattern, names []string) bool {
		if len(pattern) == 0 {
			return len(names) == 0
		}
		if pattern[0] == "**" {
			return match(pattern[1:], names) || (len(names) > 0 && match(pattern, names[1:]))
		}
		if len(names) == 0 {
			return false
		}
		ok, _ := path.Match(pattern[0], names[0])
		return ok && match(pattern[1:], names[1:])
	}
	for _, pattern := range patterns {
		if match(strings.Split(strings.Trim(filepath.ToSlash(pattern), "/"), "/"), parts) {
			return true
		}
	}
	return false
}

func gitDir(p string) (string, error) {
	marker := filepath.Join(p, ".git")
	info, err := os.Stat(marker)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return NormalizePath(marker), nil
	}
	b, err := os.ReadFile(marker)
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(b))
	if !strings.HasPrefix(line, "gitdir:") {
		return "", fmt.Errorf("invalid gitdir file %s", marker)
	}
	dir := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
	if dir == "" {
		return "", fmt.Errorf("empty gitdir file %s", marker)
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(p, dir)
	}
	return NormalizePath(dir), nil
}

func commonDir(dir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, "commondir"))
	if errors.Is(err, fs.ErrNotExist) {
		return dir, nil
	}
	if err != nil {
		return "", err
	}
	p := strings.TrimSpace(string(b))
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	return NormalizePath(p), nil
}

func repoAt(p string) (string, error) {
	dir, err := gitDir(p)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	// A missing cwd can have a file as its nearest existing ancestor.
	if errors.Is(err, fs.ErrInvalid) {
		return "", nil
	}
	if err != nil {
		if info, statErr := os.Stat(p); statErr == nil && !info.IsDir() {
			return "", nil
		}
		return "", err
	}
	common, err := commonDir(dir)
	if err != nil {
		return "", err
	}
	if common != dir {
		return NormalizePath(filepath.Dir(common)), nil
	}
	return NormalizePath(p), nil
}

type stamp struct {
	Exists bool
	Mtime  time.Time
}
type worktreeCache struct {
	Worktree   model.Worktree
	Times      [3]stamp
	FullPasses int
}
type repoCache struct {
	Entries  []WorktreeEntry
	Topology stamp
}

// Scanner caches local Git state and bounds Git subprocess concurrency across
// all concurrent Scan calls. Construct it with NewScanner.
type Scanner struct {
	mu          sync.Mutex
	cache       map[string]worktreeCache
	repos       map[string]repoCache
	locks       map[string]*sync.Mutex
	sem         chan struct{}
	concurrency int
	runGit      func(ctx context.Context, dir string, args ...string) ([]byte, error)
}

// NewScanner creates a scanner with at most concurrency Git commands in flight.
func NewScanner(concurrency int) *Scanner {
	if concurrency < 1 {
		concurrency = 1
	}
	return &Scanner{
		cache:       make(map[string]worktreeCache),
		repos:       make(map[string]repoCache),
		locks:       make(map[string]*sync.Mutex),
		sem:         make(chan struct{}, concurrency),
		concurrency: concurrency,
		runGit:      runGit,
	}
}

func runGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	out, err := platform.ChildOutput(cmd, false)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return out, err
}

func (s *Scanner) git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case s.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-s.sem }()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := s.runGit(ctx, dir, append([]string{"-c", "core.quotepath=off"}, args...)...)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, fmt.Errorf("git %s in %s: %w", strings.Join(args, " "), dir, err)
	}
	return out, nil
}

func statTime(p string) stamp {
	info, err := os.Stat(p)
	if err != nil {
		return stamp{}
	}
	return stamp{Exists: true, Mtime: info.ModTime()}
}

func worktreeTimes(p string) [3]stamp {
	dir, err := gitDir(p)
	if err != nil {
		return [3]stamp{}
	}
	return [3]stamp{statTime(filepath.Join(dir, "HEAD")), statTime(filepath.Join(dir, "index")), statTime(filepath.Join(dir, "logs", "HEAD"))}
}

func topologyTime(p string) stamp {
	dir, err := gitDir(p)
	if err != nil {
		// A bare repo is itself a Git directory.
		dir = p
	}
	common, err := commonDir(dir)
	if err != nil {
		return stamp{}
	}
	return statTime(filepath.Join(common, "worktrees"))
}

// Scan reads the repository's non-bare worktrees. A full scan bypasses metadata
// caching; its caller schedules one at least every five minutes.
func (s *Scanner) Scan(ctx context.Context, repoPath string, full bool) ([]model.Worktree, error) {
	repoPath = NormalizePath(repoPath)
	s.mu.Lock()
	lock := s.locks[repoPath]
	if lock == nil {
		lock = &sync.Mutex{}
		s.locks[repoPath] = lock
	}
	s.mu.Unlock()
	lock.Lock()
	defer lock.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	topology := topologyTime(repoPath)
	s.mu.Lock()
	r, known := s.repos[repoPath]
	refresh := full || !known || r.Topology != topology
	if !refresh {
		for _, entry := range r.Entries {
			if entry.Bare {
				continue
			}
			cached, ok := s.cache[entry.Path]
			if !ok || cached.Times != worktreeTimes(entry.Path) {
				refresh = true
				break
			}
		}
	}
	s.mu.Unlock()
	if refresh {
		out, err := s.git(ctx, repoPath, "worktree", "list", "--porcelain")
		if err != nil {
			return nil, err
		}
		r = repoCache{Entries: ParseWorktreeList(out), Topology: topology}
		for i := range r.Entries {
			r.Entries[i].Path = NormalizePath(r.Entries[i].Path)
		}
		s.mu.Lock()
		s.repos[repoPath] = r
		s.mu.Unlock()
	}
	var entries []WorktreeEntry
	for _, entry := range r.Entries {
		if entry.Bare {
			continue
		}
		// A linked worktree whose directory was deleted stays listed until
		// `git worktree prune`; it has nothing to scan.
		if _, err := os.Stat(entry.Path); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	results := make([]model.Worktree, len(entries))
	errs := make([]error, len(entries))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for n := 0; n < s.concurrency && n < len(entries); n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				results[i], errs[i] = s.scanWorktree(ctx, entries[i], full)
			}
		}()
	}
	for i := range entries {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	var worktrees []model.Worktree
	for i, w := range results {
		if errs[i] == nil {
			worktrees = append(worktrees, w)
		}
	}
	sort.Slice(worktrees, func(i, j int) bool { return worktrees[i].Path < worktrees[j].Path })
	return worktrees, errors.Join(errs...)
}

func (s *Scanner) scanWorktree(ctx context.Context, entry WorktreeEntry, full bool) (model.Worktree, error) {
	times := worktreeTimes(entry.Path)
	s.mu.Lock()
	cached, ok := s.cache[entry.Path]
	s.mu.Unlock()
	if !full && ok && cached.Times == times {
		return cached.Worktree, nil
	}
	out, err := s.git(ctx, entry.Path, "status", "--porcelain=v2", "--branch", "--untracked-files=no")
	if err != nil {
		return model.Worktree{}, err
	}
	status := ParseStatusV2(out)
	w := model.Worktree{Path: entry.Path, Branch: status.Branch, Head: entry.Head, Ahead: status.Ahead, Behind: status.Behind, Dirty: status.Dirty, Untracked: cached.Worktree.Untracked, UpdatedAt: time.Now()}
	if strings.Trim(w.Head, "0") == "" {
		w.Head = ""
	}
	passes := cached.FullPasses
	if full {
		passes++
	}
	// Counting untracked files walks the tree, so it runs on the first full
	// scan (before it the count is unknown) and then every fifth.
	if full && (passes == 1 || passes%5 == 0) {
		untracked, err := s.git(ctx, entry.Path, "status", "--porcelain=v2", "--branch", "--untracked-files=normal")
		if err != nil {
			return model.Worktree{}, err
		}
		w.Untracked = 0
		for _, line := range strings.Split(string(untracked), "\n") {
			if strings.HasPrefix(line, "? ") {
				w.Untracked++
			}
		}
	}
	for _, p := range status.DirtyPaths {
		if info, err := os.Lstat(filepath.Join(entry.Path, p)); err == nil && (w.OldestDirtyMtime.IsZero() || info.ModTime().Before(w.OldestDirtyMtime)) {
			w.OldestDirtyMtime = info.ModTime()
		}
	}
	log, err := s.git(ctx, entry.Path, "log", "-1", "--format=%ct%x00%s")
	if err != nil && (w.Head != "" || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		return model.Worktree{}, err
	}
	if err == nil {
		seconds, subject, ok := strings.Cut(strings.TrimSuffix(string(log), "\n"), "\x00")
		if !ok {
			return model.Worktree{}, fmt.Errorf("invalid git log output in %s", entry.Path)
		}
		ts, err := strconv.ParseInt(seconds, 10, 64)
		if err != nil {
			return model.Worktree{}, fmt.Errorf("invalid commit timestamp: %w", err)
		}
		w.LastCommitAt, w.LastCommitSubject = time.Unix(ts, 0), subject
	}
	s.mu.Lock()
	s.cache[entry.Path] = worktreeCache{Worktree: w, Times: times, FullPasses: passes}
	s.mu.Unlock()
	return w, nil
}
