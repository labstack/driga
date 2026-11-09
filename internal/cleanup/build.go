package cleanup

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

func checkouts(s snapshot) []string {
	seen := map[string]bool{}
	add := func(path string) {
		if !filepath.IsAbs(path) {
			return
		}
		path = resolved(path)
		for _, part := range strings.Split(filepath.ToSlash(path), "/") {
			if part == "node_modules" {
				return
			}
		}
		if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
			seen[path] = true
		}
	}
	for _, r := range s.repos {
		add(r.Path)
	}
	for _, w := range s.worktrees {
		add(w.Path)
	}
	parents := map[string]bool{}
	for _, r := range s.repos {
		parent := filepath.Dir(r.Path)
		if parents[parent] {
			continue
		}
		parents[parent] = true
		entries, _ := os.ReadDir(parent)
		for _, entry := range entries {
			if !entry.IsDir() || entry.Name() == "node_modules" || entry.Name() == ".git" {
				continue
			}
			path := filepath.Join(parent, entry.Name())
			add(path)
			children, _ := os.ReadDir(path)
			for _, child := range children {
				if child.IsDir() && child.Name() != "node_modules" && child.Name() != ".git" {
					add(filepath.Join(path, child.Name()))
				}
			}
		}
	}
	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	return paths
}

func ignoredBuildPaths(ctx context.Context, e Env, checkout string) ([]string, error) {
	out, err := e.git(ctx, checkout, "status", "--porcelain=v1", "-z", "--ignored=matching", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	paths := []string{}
	for _, entry := range strings.Split(string(out), "\x00") {
		if strings.HasPrefix(entry, "!! ") && strings.HasSuffix(entry, "/") {
			paths = append(paths, entry[3:])
		}
	}
	return paths, nil
}

type checkoutState struct {
	ignored    []string
	lastCommit time.Time
	commitErr  error
}

func readCheckoutState(ctx context.Context, e Env, checkout string) (checkoutState, error) {
	paths, err := ignoredBuildPaths(ctx, e, checkout)
	if err != nil {
		return checkoutState{}, err
	}
	state := checkoutState{ignored: paths}
	out, err := e.git(ctx, checkout, "log", "-1", "--format=%ct")
	if err != nil {
		state.commitErr = errors.New("checkout has no commits")
		return state, nil
	}
	sec, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	state.lastCommit, state.commitErr = time.Unix(sec, 0), err
	return state, nil
}

func buildCandidates(ctx context.Context, e Env, s snapshot) []Candidate {
	if runtime.GOOS == "windows" {
		return nil
	}
	var cs []Candidate
	for _, checkout := range checkouts(s) {
		state, err := readCheckoutState(ctx, e, checkout)
		if err != nil {
			continue
		}
		for _, rel := range state.ignored {
			// Unknown ignored directories are not build candidates, even with --why.
			if _, err := qualifyBuild(checkout, rel, e.Cfg.Regenerable); err != nil {
				if errors.Is(err, errNotBuild) {
					continue
				}
			}
			c, err := inspectBuild(ctx, e, s, state, checkout, rel)
			if err != nil {
				if !e.Why {
					continue
				}
				c.Status, c.Evidence = "refused", []string{err.Error()}
			}
			cs = append(cs, c)
		}
	}
	return cs
}

func inspectBuild(ctx context.Context, e Env, s snapshot, state checkoutState, checkout, rel string) (Candidate, error) {
	path := filepath.Join(checkout, rel)
	c := Candidate{ID: fmt.Sprintf("build:%x", sha256.Sum256([]byte(path)))[:18], Kind: Build, Path: path, Repo: checkout, root: checkout, Status: "ready", Ignored: []IgnoredPath{}}
	fail := func(reason string) (Candidate, error) { return c, fmt.Errorf("%s", reason) }
	if !filepath.IsAbs(checkout) || !filepath.IsLocal(strings.TrimSuffix(rel, "/")) || filepath.Clean(rel) == "." {
		return c, errNotBuild
	}
	info, err := os.Lstat(path)
	if err != nil {
		return c, err
	}
	if info.Mode()&os.ModeSymlink != 0 || resolved(path) != filepath.Clean(path) {
		return fail("symlink")
	}
	if !info.IsDir() {
		return c, errNotBuild
	}
	rule, err := qualifyBuild(checkout, filepath.ToSlash(strings.TrimSuffix(rel, "/"))+"/", e.Cfg.Regenerable)
	if err != nil {
		return c, err
	}
	if !slices.Contains(state.ignored, filepath.ToSlash(strings.TrimSuffix(rel, "/"))+"/") {
		return fail("not an ignored directory")
	}
	walkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := walkCtx.Err(); err != nil {
			return err
		}
		if d.Name() == ".git" {
			return fmt.Errorf("contains a nested git repository")
		}
		return nil
	})
	if err != nil {
		return c, err
	}
	if err := walkCtx.Err(); err != nil {
		return c, err
	}
	for _, cwd := range s.cwds {
		if inside(path, resolved(cwd)) {
			return fail("process cwd is inside")
		}
	}
	open, err := openUnder(s, path)
	if err != nil {
		return fail("open-file inspection unavailable")
	}
	if open {
		return fail("process has an open file inside")
	}
	latest := time.Time{}
	for _, session := range s.sessions {
		if !inside(checkout, resolved(session.Cwd)) {
			continue
		}
		if sessionLive(e, session) {
			return fail("live session in checkout")
		}
		if session.LastActivityAt.After(latest) {
			latest = session.LastActivityAt
		}
		if session.EndedAt != nil && session.EndedAt.After(latest) {
			latest = *session.EndedAt
		}
	}
	if state.commitErr != nil {
		return c, state.commitErr
	}
	if state.lastCommit.After(latest) {
		latest = state.lastCommit
	}
	if e.now().Sub(latest) < e.Cfg.BuildIdle {
		return fail("checkout active within cleanup.build_idle")
	}
	c.identity, err = captureIdentity(path)
	if err != nil {
		return c, err
	}
	evidence := "user setting"
	for _, r := range buildRules {
		if r.Dir != rule {
			continue
		}
		evidence = "no evidence required"
		for _, glob := range r.Evidence {
			matches, _ := filepath.Glob(filepath.Join(filepath.Dir(path), glob))
			if len(matches) > 0 {
				evidence = filepath.Base(matches[0])
				break
			}
		}
	}
	c.Evidence = []string{rule + " beside " + evidence, "checkout idle since " + latest.UTC().Format(time.RFC3339)}
	return c, nil
}
