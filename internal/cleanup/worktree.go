package cleanup

import (
	"context"
	"fmt"
	"github.com/labstack/driga/internal/model"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const errMainWorktree = "main worktree is never removable"

func inspectWorktree(ctx context.Context, e Env, s snapshot, w model.Worktree, expected *Candidate) (Candidate, error) {
	c := Candidate{ID: wtID(w.Path), Kind: Worktree, Path: w.Path, Status: "ready", Ignored: []IgnoredPath{}}
	fail := func(reason string) (Candidate, error) { return c, fmt.Errorf("%s", reason) }
	if !filepath.IsAbs(w.Path) {
		return fail("worktree path is not absolute")
	}
	for _, r := range s.repos {
		if r.ID == w.RepoID {
			c.Repo = r.Path
			break
		}
	}
	if c.Repo == "" {
		return fail("repository no longer attributed")
	}
	out, err := e.git(ctx, c.Repo, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return c, err
	}
	first, matched, locked := true, false, false
	for _, record := range strings.Split(string(out), "\x00\x00") {
		fields := strings.Split(record, "\x00")
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "worktree ") {
			continue
		}
		path := strings.TrimPrefix(fields[0], "worktree ")
		if resolved(path) == resolved(w.Path) {
			if first {
				return fail(errMainWorktree)
			}
			matched = true
			for _, f := range fields {
				if f == "locked" || strings.HasPrefix(f, "locked ") {
					locked = true
				}
				if strings.HasPrefix(f, "branch refs/heads/") {
					c.Branch = strings.TrimPrefix(f, "branch refs/heads/")
				}
			}
		}
		first = false
	}
	if !matched {
		return fail("not a linked worktree")
	}
	if locked {
		return fail("worktree is locked")
	}
	c.identity, err = os.Stat(c.Path)
	if err != nil {
		return c, err
	}
	get := func(args ...string) (string, error) {
		out, err := e.git(ctx, c.Path, args...)
		return strings.TrimSpace(string(out)), err
	}
	if c.head, err = get("rev-parse", "HEAD"); err != nil {
		return c, err
	}
	if expected != nil && (c.head != expected.head || c.Branch != expected.Branch) {
		return fail("HEAD changed since listing")
	}
	out, err = e.git(ctx, c.Path, "status", "--porcelain=v1", "-z", "--ignored=matching", "--untracked-files=all")
	if err != nil {
		return c, err
	}
	for _, entry := range strings.Split(string(out), "\x00") {
		if entry == "" {
			continue
		}
		if len(entry) < 3 {
			return fail("unreadable git status")
		}
		switch entry[:2] {
		case "??":
			return fail("untracked files appeared")
		case "!!":
			path := entry[3:]
			regen := false
			// Only whole ignored directories from the allowlist are regenerable.
			// A file called 'build' or '.env' never inherits that permission.
			if strings.HasSuffix(path, "/") {
				for _, rule := range e.Cfg.Regenerable {
					if strings.HasSuffix(rule, "/") && strings.HasSuffix("/"+path, "/"+rule) {
						regen = true
					}
				}
			}
			c.Ignored = append(c.Ignored, IgnoredPath{Path: path, Regenerable: regen})
			if !regen {
				c.Status = "needs_review"
			}
		default:
			return fail("worktree has tracked changes")
		}
	}
	for _, cwd := range s.cwds {
		if inside(resolved(c.Path), resolved(cwd)) {
			return fail("process cwd is inside worktree")
		}
	}
	latest := time.Time{}
	for _, session := range s.sessions {
		if inside(resolved(c.Path), resolved(session.Cwd)) {
			if session.Label != "dead" && session.Label != "finished" {
				return fail("live session is inside worktree")
			}
			if session.LastActivityAt.After(latest) {
				latest = session.LastActivityAt
			}
			if session.EndedAt != nil && session.EndedAt.After(latest) {
				latest = *session.EndedAt
			}
		}
	}
	// Prefer the remote's declared default; local main/master are conservative fallbacks.
	defaultRef, _ := get("symbolic-ref", "refs/remotes/origin/HEAD")
	if defaultRef == "" {
		for _, ref := range []string{"refs/heads/main", "refs/heads/master"} {
			if _, err := get("rev-parse", "--verify", ref); err == nil {
				defaultRef = ref
				break
			}
		}
	}
	if defaultRef != "" {
		c.defaultHead, err = get("rev-parse", "--verify", defaultRef)
		if err != nil {
			return c, err
		}
		_, err = e.git(ctx, c.Path, "merge-base", "--is-ancestor", c.head, c.defaultHead)
		c.merged = err == nil
	}
	c.upstream, _ = get("rev-parse", "--symbolic-full-name", "@{u}")
	pushed := false
	if c.upstream != "" {
		c.upstreamHead, err = get("rev-parse", "--verify", c.upstream)
		if err != nil {
			return c, err
		}
		_, err = e.git(ctx, c.Path, "merge-base", "--is-ancestor", c.head, c.upstreamHead)
		pushed = err == nil
	}
	if !c.merged && !pushed {
		return fail("HEAD is neither merged into default branch nor fully pushed to upstream")
	}
	if expected != nil && (expected.defaultHead != c.defaultHead || expected.upstream != c.upstream || expected.upstreamHead != c.upstreamHead) {
		return fail("default branch or upstream changed since listing")
	}
	stamp, err := get("log", "-1", "--format=%ct")
	if err != nil {
		return c, err
	}
	sec, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil {
		return c, err
	}
	if commit := time.Unix(sec, 0); commit.After(latest) {
		latest = commit
	}
	index, err := get("rev-parse", "--git-path", "index")
	if err != nil {
		return c, err
	}
	if !filepath.IsAbs(index) {
		index = filepath.Join(c.Path, index)
	}
	info, err := os.Stat(index)
	if err != nil {
		return c, err
	}
	c.indexTime = info.ModTime()
	if c.indexTime.After(latest) {
		latest = c.indexTime
	}
	if expected != nil && !c.indexTime.Equal(expected.indexTime) {
		return fail("git index changed since listing")
	}
	if latest.IsZero() || e.now().Sub(latest) < e.Cfg.WorktreeIdle {
		return fail("worktree has not reached idle threshold")
	}
	c.Evidence = []string{"linked, unlocked; clean including untracked files", fmt.Sprintf("idle since %s", latest.UTC().Format(time.RFC3339)), "no live session or process cwd inside"}
	if c.merged {
		c.Evidence = append(c.Evidence, "HEAD reachable from "+defaultRef)
	} else {
		c.Evidence = append(c.Evidence, "HEAD fully pushed to "+c.upstream)
	}
	return c, nil
}
