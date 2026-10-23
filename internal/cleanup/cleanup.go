// Package cleanup inspects local candidates read-only and rechecks them at action time.
package cleanup

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/labstack/driga/internal/secfile"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/logfile"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/pathx"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/signatures"
	"github.com/labstack/driga/internal/store"
)

type Kind string

const (
	Worktree Kind = "worktree"
	Scratch  Kind = "scratch"
	History  Kind = "history"
)

type IgnoredPath struct {
	Path        string `json:"path"`
	Regenerable bool   `json:"regenerable"`
	Size        int64  `json:"size_bytes"`
}
type Candidate struct {
	ID                                        string        `json:"id"`
	Kind                                      Kind          `json:"kind"`
	Path                                      string        `json:"path"`
	Repo                                      string        `json:"repo,omitempty"`
	Branch                                    string        `json:"branch,omitempty"`
	Evidence                                  []string      `json:"evidence"`
	Status                                    string        `json:"status"`
	Size                                      int64         `json:"size_bytes"`
	SizeCapped                                bool          `json:"size_capped"`
	Ignored                                   []IgnoredPath `json:"ignored"`
	head, defaultHead, upstream, upstreamHead string
	indexTime                                 time.Time
	merged                                    bool
	root                                      string
	rootCanonical                             string
	rootIdentity                              os.FileInfo
	identity                                  *fileIdentity
}
type Env struct {
	Store         *store.Store
	Plat          platform.Platform
	Sigs          *signatures.Set
	Home, DataDir string
	Now           time.Time
	Cfg           config.CleanupConfig
	Git           func(context.Context, string, ...string) ([]byte, error)
	Sizes         bool
	// Why also lists worktrees that do not qualify, with their refusal reason.
	Why bool
	// TempRoots replaces OS temp roots in isolated tests; layout remains binding.
	TempRoots []string
	// Rename is an injectable same_filesystem rename, never a copy fallback.
	Rename func(string, string) error
}
type ApplyOpts struct{ DryRun, IncludeIgnored bool }
type Result struct {
	ID        string     `json:"id"`
	Commands  [][]string `json:"commands"`
	TrashPath string     `json:"trash_path,omitempty"`
	DryRun    bool       `json:"dry_run"`
	Warnings  []string   `json:"warnings,omitempty"`
}

func (e Env) now() time.Time {
	if e.Now.IsZero() {
		return time.Now()
	}
	return e.Now
}
func runGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	out, err := platform.ChildOutput(cmd, true)
	if err != nil {
		return nil, fmt.Errorf("git -C %q %v: %w: %s", dir, args, err, strings.TrimSpace(string(out)))
	}
	return out, nil
}
func (e Env) git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	if e.Git != nil {
		return e.Git(ctx, dir, args...)
	}
	return runGit(ctx, dir, args...)
}
func inside(root, path string) bool {
	if !filepath.IsAbs(root) || !filepath.IsAbs(path) {
		return false
	}
	return pathx.Within(path, root)
}
func resolved(path string) string {
	p, err := filepath.EvalSymlinks(path)
	if err != nil {
		// A cwd beneath a renamed/missing child still needs its existing
		// ancestors canonicalized (notably /var -> /private/var on macOS).
		parent := filepath.Dir(path)
		if filepath.IsAbs(path) && parent != path {
			return filepath.Join(resolved(parent), filepath.Base(path))
		}
		return filepath.Clean(path)
	}
	return p
}
func wtID(path string) string { return fmt.Sprintf("wt:%x", sha256.Sum256([]byte(path)))[:15] }

type snapshot struct {
	repos      []model.Repo
	worktrees  []model.Worktree
	sessions   []model.Session
	cwds       map[int32]string
	unreadable int
}

func read(ctx context.Context, e Env) (snapshot, error) {
	var s snapshot
	if e.Store == nil || e.Plat == nil {
		return s, fmt.Errorf("cleanup requires local store and process inspection")
	}
	err := e.Store.Tx(ctx, func(tx *store.Tx) error {
		var err error
		if s.repos, err = tx.Repos(); err != nil {
			return err
		}
		if s.worktrees, err = tx.Worktrees(0); err != nil {
			return err
		}
		s.sessions, err = tx.Sessions(store.SessionFilter{IncludeEnded: true})
		return err
	})
	if err != nil {
		return s, err
	}
	procs, err := e.Plat.Procs(ctx)
	if err != nil {
		return s, err
	}
	sigs := e.Sigs
	if sigs == nil {
		sigs, err = signatures.Load(nil)
		if err != nil {
			return s, err
		}
	}
	// Only the user's own processes can be inspected (other users' cwd is
	// unreadable without root), so they alone gate cleanup. A process whose
	// owner is unknown is treated as the user's own.
	uid := uint32(os.Getuid())
	own := procs[:0:0]
	pids := make([]int32, 0, len(procs))
	for _, p := range procs {
		// An unreaped process has exited and cannot hold a cwd or open files.
		if p.Zombie || p.OwnerKnown && p.OwnerUID != uid {
			continue
		}
		own = append(own, p)
		pids = append(pids, p.PID)
	}
	s.cwds, err = e.Plat.Cwds(ctx, pids)
	if err == nil {
		for _, p := range own {
			if s.cwds[p.PID] == "" && e.Plat.Alive(p.PID, p.StartTime) {
				// Only a process identified as a non-agent may be skipped: one
				// with no readable command line could be an agent.
				sig, ok := sigs.Match(p.Cmdline, p.Exe)
				if p.Cmdline == "" && p.Exe == "" || ok && agentSignature(sig) {
					return s, fmt.Errorf("cannot inspect cwd for live process %d", p.PID)
				}
				s.unreadable++
			}
		}
	}
	return s, err
}

func agentSignature(sig signatures.Signature) bool {
	if sig.Agent != "" {
		return true
	}
	switch sig.Role {
	case signatures.RoleInteractive, signatures.RoleHeadless, signatures.RoleHost, signatures.RoleHelper, signatures.RoleInternal, signatures.RoleService:
		return true
	}
	return false
}

func List(ctx context.Context, e Env) ([]Candidate, error) {
	s, err := read(ctx, e)
	if err != nil {
		return nil, err
	}
	cs := []Candidate{}
	for _, w := range s.worktrees {
		c, err := inspectWorktree(ctx, e, s, w, nil)
		if err == nil {
			cs = append(cs, c)
		} else if e.Why && err.Error() != errMainWorktree {
			// Main worktrees are never candidates; listing each one is noise.
			cs = append(cs, Candidate{ID: wtID(w.Path), Kind: Worktree, Path: w.Path, Repo: c.Repo, Branch: c.Branch, Status: "refused", Evidence: []string{err.Error()}, Ignored: []IgnoredPath{}})
		}
	}
	if s.unreadable > 0 {
		for i := range cs {
			cs[i].Evidence = append(cs[i].Evidence, fmt.Sprintf("%d processes with unreadable cwd were not checked", s.unreadable))
		}
	}
	cs = append(cs, scratchCandidates(ctx, e, s)...)
	cs = append(cs, historyCandidates(e)...)
	if e.Sizes {
		for i := range cs {
			if cs[i].Status == "refused" {
				continue
			}
			cs[i], err = Measure(ctx, cs[i], e.now())
			if err != nil {
				cs[i].Evidence = append(cs[i].Evidence, "size: "+err.Error())
			}
		}
	}
	return cs, nil
}

// verify returns the freshly inspected candidate; no listed boolean authorizes IO.
func verify(ctx context.Context, e Env, c Candidate) (Candidate, error) {
	if c.Kind == History {
		return Candidate{}, fmt.Errorf("agent history is report-only; use the agent's retention setting")
	}
	if c.Status == "refused" {
		return Candidate{}, fmt.Errorf("candidate was refused when listed (%s); list again", strings.Join(c.Evidence, "; "))
	}
	if c.identity == nil {
		return Candidate{}, fmt.Errorf("candidate must be listed before applying")
	}
	s, err := read(ctx, e)
	if err != nil {
		return Candidate{}, err
	}
	var fresh Candidate
	switch c.Kind {
	case Worktree:
		found := false
		for _, w := range s.worktrees {
			if w.Path == c.Path {
				found = true
				fresh, err = inspectWorktree(ctx, e, s, w, &c)
				break
			}
		}
		if !found {
			return fresh, fmt.Errorf("worktree no longer attributed")
		}
	case Scratch:
		fresh, err = inspectScratch(ctx, e, s, c)
	default:
		return fresh, fmt.Errorf("unknown cleanup kind")
	}
	if err != nil {
		return fresh, err
	}
	if c.ID != fresh.ID || c.Path != fresh.Path || !sameIdentity(c.identity, fresh.identity) {
		return fresh, fmt.Errorf("candidate path identity changed")
	}
	if !reflect.DeepEqual(c.Ignored, fresh.Ignored) {
		// Sizes are presentation-only; compare paths and classifications instead.
		a, b := append([]IgnoredPath(nil), c.Ignored...), append([]IgnoredPath(nil), fresh.Ignored...)
		for i := range a {
			a[i].Size = 0
		}
		for i := range b {
			b[i].Size = 0
		}
		if !reflect.DeepEqual(a, b) {
			return fresh, fmt.Errorf("ignored files changed; list and review again")
		}
	}
	return fresh, nil
}
func Verify(ctx context.Context, e Env, c Candidate) error { _, err := verify(ctx, e, c); return err }
func Apply(ctx context.Context, e Env, c Candidate, opts ApplyOpts) (r Result, err error) {
	r = Result{ID: c.ID, DryRun: opts.DryRun}
	// Prepare the audit sink before any destructive operation. CLI never writes SQLite.
	if mkErr := secfile.MkdirPrivate(e.DataDir); mkErr != nil {
		return r, mkErr
	}
	log, openErr := secfile.OpenFile(filepath.Join(e.DataDir, "cleanup.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY)
	if openErr != nil {
		return r, openErr
	}
	defer func() {
		entry := struct {
			At        time.Time `json:"occurred_at"`
			Candidate Candidate `json:"candidate"`
			Result    Result    `json:"result"`
			Error     string    `json:"error,omitempty"`
		}{At: e.now().UTC(), Candidate: c, Result: r}
		if err != nil {
			entry.Error = err.Error()
		}
		// The sink was checked before acting, so a failed write afterwards must
		// not report a completed action as failed.
		logErr := errors.Join(json.NewEncoder(&logfile.Writer{Path: filepath.Join(e.DataDir, "cleanup.log")}).Encode(entry), log.Close())
		if logErr != nil && err == nil {
			r.Warnings = append(r.Warnings, "audit log: "+logErr.Error())
		}
	}()
	fresh, err := verify(ctx, e, c)
	if err != nil {
		return r, err
	}
	if fresh.Status == "needs_review" && !opts.IncludeIgnored {
		return r, fmt.Errorf("ignored non-regenerable files require --include-ignored for this candidate")
	}
	if fresh.Kind == Worktree {
		// Prune before the branch: once the worktree is gone, the action has
		// succeeded even if git judges the branch unmerged against its upstream.
		r.Commands = [][]string{
			{"git", "-C", fresh.Repo, "worktree", "remove", "--", fresh.Path},
			{"git", "-C", fresh.Repo, "worktree", "prune"},
		}
		if fresh.merged && fresh.Branch != "" {
			r.Commands = append(r.Commands, []string{"git", "-C", fresh.Repo, "branch", "-d", "--", fresh.Branch})
		}
		if runtime.GOOS == "windows" {
			gitCommands := r.Commands[1:]
			r, err = trash(ctx, e, fresh, opts, r)
			if err != nil {
				return r, err
			}
			r.Commands = append(r.Commands, gitCommands...)
			if opts.DryRun {
				return r, nil
			}
			for i, cmd := range gitCommands {
				if _, gitErr := e.git(ctx, cmd[2], cmd[3:]...); gitErr != nil {
					if i == 0 {
						r.Warnings = append(r.Warnings, fmt.Sprintf("worktree recycled; prune failed: %v", gitErr))
					} else {
						r.Warnings = append(r.Warnings, fmt.Sprintf("kept branch %s: %v", fresh.Branch, gitErr))
					}
				}
			}
			return r, nil
		}
		if opts.DryRun {
			return r, nil
		}
		for i, cmd := range r.Commands {
			if _, gitErr := e.git(ctx, cmd[2], cmd[3:]...); gitErr != nil {
				if i == 0 {
					return r, gitErr
				}
				if i == 1 {
					r.Warnings = append(r.Warnings, fmt.Sprintf("worktree removed; prune failed: %v", gitErr))
				} else {
					r.Warnings = append(r.Warnings, fmt.Sprintf("kept branch %s: %v", fresh.Branch, gitErr))
				}
			}
		}
		return r, nil
	}
	return trash(ctx, e, fresh, opts, r)
}
