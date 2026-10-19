package cleanup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/logfile"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

type fakePlatform struct {
	procs    []model.Proc
	open     bool
	cwdCalls int
	pids     []int32
	probe    func()
	alive    bool
}

func (f *fakePlatform) Procs(context.Context) ([]model.Proc, error) { return f.procs, nil }
func (f *fakePlatform) Machine(context.Context) (model.MachineSample, error) {
	return model.MachineSample{}, nil
}
func (f *fakePlatform) Alive(int32, time.Time) bool { return f.alive }
func (f *fakePlatform) Cwds(_ context.Context, pids []int32) (map[int32]string, error) {
	f.cwdCalls++
	f.pids = pids
	m := map[int32]string{}
	for _, p := range f.procs {
		m[p.PID] = p.Cwd
	}
	return m, nil
}
func (f *fakePlatform) OpenFiles(context.Context, string) (bool, error) {
	if f.probe != nil {
		f.probe()
	}
	return f.open, nil
}

func TestScratchRootSwapDuringVerify(t *testing.T) {
	e, p, _, _ := fixture(t)
	path := scratch(t, e, "swap")
	c := listed(t, e, path)
	outside := t.TempDir()
	put(t, filepath.Join(outside, "swap", "keep"), "outside")
	root := filepath.Dir(path)
	moved := root + "-original"
	p.probe = func() {
		p.probe = nil
		if err := os.Rename(root, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, root); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Apply(context.Background(), e, c, ApplyOpts{}); err == nil {
		t.Fatal("root replacement during verification accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "swap", "keep")); err != nil {
		t.Fatal("outside source moved", err)
	}
}

func TestScratchTrashSymlinkRefused(t *testing.T) {
	e, _, _, _ := fixture(t)
	path := scratch(t, e, "trash-link")
	c := listed(t, e, path)
	files, _, err := trashDirs(e.Home)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(files), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), files); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(context.Background(), e, c, ApplyOpts{}); err == nil {
		t.Fatal("symlink trash destination accepted")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_DATE=2026-09-01T12:00:00Z", "GIT_COMMITTER_DATE=2026-09-01T12:00:00Z")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %q: %s %v", args, out, err)
	}
	return strings.TrimSpace(string(out))
}
func put(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0600); err != nil {
		t.Fatal(err)
	}
}
func tx(t *testing.T, e Env, f func(*store.Tx) error) {
	t.Helper()
	if err := e.Store.Tx(context.Background(), f); err != nil {
		t.Fatal(err)
	}
}
func fixture(t *testing.T) (Env, *fakePlatform, string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	p := &fakePlatform{}
	e := Env{Store: st, Plat: p, Home: home, DataDir: t.TempDir(), Now: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC), Cfg: config.Default().Cleanup, TempRoots: []string{os.Getenv("TMPDIR")}}
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0700); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-b", "main")
	git(t, repo, "config", "user.email", "test@example.invalid")
	git(t, repo, "config", "user.name", "Test")
	put(t, filepath.Join(repo, "tracked"), "one")
	put(t, filepath.Join(repo, ".gitignore"), "node_modules/\n.env\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "base")
	wt := filepath.Join(t.TempDir(), "linked")
	git(t, repo, "worktree", "add", "-b", "topic", wt)
	tx(t, e, func(tx *store.Tx) error {
		id, err := tx.UpsertRepo(model.Repo{Path: repo, Name: "test"})
		if err != nil {
			return err
		}
		for _, path := range []string{repo, wt} {
			if _, err = tx.UpsertWorktree(model.Worktree{RepoID: id, Path: path}); err != nil {
				return err
			}
		}
		return nil
	})
	ageIndex(t, e, wt)
	return e, p, repo, wt
}
func ageIndex(t *testing.T, e Env, wt string) {
	t.Helper()
	p := git(t, wt, "rev-parse", "--git-path", "index")
	if !filepath.IsAbs(p) {
		p = filepath.Join(wt, p)
	}
	old := e.Now.Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
}
func listed(t *testing.T, e Env, path string) Candidate {
	t.Helper()
	cs, err := List(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		if c.Path == path {
			return c
		}
	}
	t.Fatalf("missing %s: %+v", path, cs)
	return Candidate{}
}
func absent(t *testing.T, e Env, path string) {
	t.Helper()
	cs, err := List(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		if c.Path == path {
			t.Fatalf("unsafe candidate: %+v", c)
		}
	}
}

func TestWorktreeCandidateReady(t *testing.T) {
	e, p, _, wt := fixture(t)
	c := listed(t, e, wt)
	if c.Kind != Worktree || c.Status != "ready" || !strings.HasPrefix(c.ID, "wt:") || len(c.Evidence) == 0 {
		t.Fatal(c)
	}
	if p.cwdCalls != 1 {
		t.Fatal("expected single cwd snapshot", p.cwdCalls)
	}
}
func TestWorktreeRefusals(t *testing.T) {
	for _, name := range []string{"main", "locked", "dirty", "untracked", "unpushed", "no-upstream", "idle-6d", "process", "live-session"} {
		t.Run(name, func(t *testing.T) {
			e, p, repo, wt := fixture(t)
			switch name {
			case "main":
				wt = repo
			case "locked":
				git(t, repo, "worktree", "lock", wt)
			case "dirty":
				put(t, filepath.Join(wt, "tracked"), "change")
			case "untracked":
				put(t, filepath.Join(wt, "new"), "work")
			case "unpushed", "no-upstream":
				git(t, wt, "commit", "--allow-empty", "-m", "unmerged")
				if name == "unpushed" {
					git(t, wt, "config", "branch.topic.remote", ".")
					git(t, wt, "config", "branch.topic.merge", "refs/heads/main")
				}
				ageIndex(t, e, wt)
			case "idle-6d":
				e.Now = e.Now.Add(-2 * 24 * time.Hour)
			case "process":
				p.procs = []model.Proc{{PID: 123, Cwd: filepath.Join(wt, "sub")}}
			case "live-session":
				tx(t, e, func(tx *store.Tx) error {
					_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "codex", ExternalID: "live", Cwd: wt}, Label: "working"})
					return err
				})
			}
			absent(t, e, wt)
		})
	}
}
func TestWorktreeIgnoredClassification(t *testing.T) {
	e, _, _, wt := fixture(t)
	put(t, filepath.Join(wt, "node_modules", "pkg"), "build")
	put(t, filepath.Join(wt, ".env"), "SECRET")
	c := listed(t, e, wt)
	if c.Status != "needs_review" || len(c.Ignored) != 2 {
		t.Fatal(c)
	}
	for _, i := range c.Ignored {
		if i.Regenerable != strings.HasPrefix(i.Path, "node_modules/") {
			t.Fatal(i)
		}
	}
	if _, err := Apply(context.Background(), e, c, ApplyOpts{}); err == nil {
		t.Fatal("ignored data removed without consent")
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(context.Background(), e, c, ApplyOpts{IncludeIgnored: true}); err != nil {
		t.Fatal("explicit ignored consent", err)
	}
}
func TestVerifyAtActionTime(t *testing.T) {
	for _, name := range []string{"untracked", "commit", "process", "locked", "ignored", "session"} {
		t.Run(name, func(t *testing.T) {
			e, p, repo, wt := fixture(t)
			c := listed(t, e, wt)
			want := ""
			switch name {
			case "untracked":
				put(t, filepath.Join(wt, "new"), "new")
				want = "untracked files appeared"
			case "commit":
				git(t, wt, "commit", "--allow-empty", "-m", "new")
				want = "HEAD changed"
			case "process":
				p.procs = []model.Proc{{PID: 999, Cwd: wt}}
				want = "process"
			case "locked":
				git(t, repo, "worktree", "lock", wt)
				want = "locked"
			case "ignored":
				put(t, filepath.Join(wt, ".env"), "private")
				want = "ignored"
			case "session":
				tx(t, e, func(tx *store.Tx) error {
					_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "live", Cwd: wt}, Label: "working"})
					return err
				})
				want = "session"
			}
			_, err := Apply(context.Background(), e, c, ApplyOpts{IncludeIgnored: true})
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatal("expected", want, "got", err)
			}
			if _, err := os.Stat(wt); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestApplyWorktree(t *testing.T) {
	for _, pushed := range []bool{false, true} {
		t.Run(fmt.Sprint(pushed), func(t *testing.T) {
			e, _, repo, wt := fixture(t)
			if pushed {
				git(t, wt, "commit", "--allow-empty", "-m", "pushed")
				git(t, repo, "branch", "published", "topic")
				git(t, wt, "config", "branch.topic.remote", ".")
				git(t, wt, "config", "branch.topic.merge", "refs/heads/published")
				ageIndex(t, e, wt)
			}
			c := listed(t, e, wt)
			dry, err := Apply(context.Background(), e, c, ApplyOpts{DryRun: true})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(wt); err != nil {
				t.Fatal(err)
			}
			r, err := Apply(context.Background(), e, c, ApplyOpts{})
			if err != nil {
				t.Fatal(err)
			}
			if fmt.Sprint(dry.Commands) != fmt.Sprint(r.Commands) {
				t.Fatal(dry, r)
			}
			if _, err := os.Stat(wt); !os.IsNotExist(err) {
				t.Fatal(err)
			}
			branches := git(t, repo, "branch", "--list", "topic")
			if (branches != "") != pushed {
				t.Fatal("branch deletion rule", branches)
			}
			for _, cmd := range r.Commands {
				if len(cmd) < 3 || cmd[0] != "git" || cmd[1] != "-C" || strings.Contains(strings.Join(cmd, " "), "--force") || strings.Contains(strings.Join(cmd, " "), "-D") {
					t.Fatal(cmd)
				}
			}
			b, err := os.ReadFile(filepath.Join(e.DataDir, "cleanup.log"))
			if err != nil {
				t.Fatal(err)
			}
			for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
				var v map[string]any
				if err := json.Unmarshal([]byte(line), &v); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func scratch(t *testing.T, e Env, id string) string {
	t.Helper()
	end := e.Now.Add(-48 * time.Hour)
	cwd := filepath.Join(e.Home, "project")
	tx(t, e, func(tx *store.Tx) error {
		_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: id, Cwd: cwd, LastActivityAt: end}, Label: "dead", EndedAt: &end})
		return err
	})
	path := filepath.Join(e.TempRoots[0], fmt.Sprintf("claude-%d", os.Getuid()), projectSlug(cwd), id)
	put(t, filepath.Join(path, "file"), "scratch")
	return path
}
func TestScratchCandidate(t *testing.T) {
	e, _, _, _ := fixture(t)
	path := scratch(t, e, "session-one")
	c := listed(t, e, path)
	if c.ID != "scratch:session-one" || c.Status != "ready" {
		t.Fatal(c)
	}
}

func TestScratchApplyRechecksAfterVerify(t *testing.T) {
	for _, name := range []string{"open-file", "session"} {
		t.Run(name, func(t *testing.T) {
			e, p, _, _ := fixture(t)
			path := scratch(t, e, "recheck")
			c := listed(t, e, path)
			if err := Verify(context.Background(), e, c); err != nil {
				t.Fatal(err)
			}
			if name == "open-file" {
				p.open = true
			} else {
				tx(t, e, func(tx *store.Tx) error {
					_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "recheck", Cwd: filepath.Join(e.Home, "project")}, Label: "working"})
					return err
				})
			}
			if _, err := Apply(context.Background(), e, c, ApplyOpts{}); err == nil {
				t.Fatal("Apply reused an earlier verification")
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestScratchAmbiguousRoots(t *testing.T) {
	e, _, _, _ := fixture(t)
	path := scratch(t, e, "ambiguous")
	other := t.TempDir()
	e.TempRoots = append(e.TempRoots, other)
	otherPath := filepath.Join(other, fmt.Sprintf("claude-%d", os.Getuid()), projectSlug(filepath.Join(e.Home, "project")), "ambiguous")
	put(t, filepath.Join(otherPath, "keep"), "safe")
	absent(t, e, path)
	absent(t, e, otherPath)
}
func TestScratchRefusals(t *testing.T) {
	for _, name := range []string{"live", "finished-alive", "open-file", "escape", "dotdot", "slash", "unknown"} {
		t.Run(name, func(t *testing.T) {
			e, p, _, _ := fixture(t)
			id := "one"
			if name == "dotdot" {
				id = "a..b"
			}
			if name == "slash" {
				id = "a/b"
			}
			path := scratch(t, e, id)
			switch name {
			case "live":
				tx(t, e, func(tx *store.Tx) error {
					_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: id, Cwd: filepath.Join(e.Home, "project")}, Label: "working"})
					return err
				})
			case "finished-alive":
				// "finished" also labels an unacknowledged completion whose
				// terminal is still open; its scratch dir is still in use.
				p.alive = true
				tx(t, e, func(tx *store.Tx) error {
					_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: id, PID: 4242, ProcStart: e.Now.Add(-72 * time.Hour), LastActivityAt: e.Now.Add(-48 * time.Hour), Cwd: filepath.Join(e.Home, "project")}, Label: "finished"})
					return err
				})
			case "open-file":
				p.open = true
			case "escape":
				outside := t.TempDir()
				put(t, filepath.Join(outside, "keep"), "safe")
				if err := os.RemoveAll(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, path); err != nil {
					t.Fatal(err)
				}
			case "unknown":
				path = filepath.Join(filepath.Dir(path), "unowned")
				put(t, filepath.Join(path, "keep"), "safe")
			}
			absent(t, e, path)
			if _, err := Apply(context.Background(), e, Candidate{ID: "scratch:" + id, Kind: Scratch, Path: path}, ApplyOpts{}); err == nil {
				t.Fatal("forged candidate accepted")
			}
			if _, err := os.Lstat(path); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestScratchTrash(t *testing.T) {
	e, p, _, _ := fixture(t)
	path := scratch(t, e, "trash-me")
	c := listed(t, e, path)
	r, err := Apply(context.Background(), e, c, ApplyOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if !strings.HasPrefix(r.TrashPath, e.Home+string(filepath.Separator)) {
		t.Fatal(r)
	}
	if _, err := os.Stat(filepath.Join(r.TrashPath, "file")); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "linux" {
		if !strings.Contains(r.TrashPath, "/Trash/files/") {
			t.Fatal(r)
		}
		if _, err := os.Stat(filepath.Join(e.Home, ".local/share/Trash/info", filepath.Base(r.TrashPath)+".trashinfo")); err != nil {
			t.Fatal(err)
		}
	}
	path = scratch(t, e, "blocked")
	c = listed(t, e, path)
	e.Home = filepath.Join(t.TempDir(), "not-dir")
	put(t, e.Home, "block")
	if _, err := Apply(context.Background(), e, c, ApplyOpts{}); err == nil {
		t.Fatal("unavailable trash accepted")
	}
	_ = p
}
func TestTrashCrossDeviceRefuses(t *testing.T) {
	e, _, _, _ := fixture(t)
	path := scratch(t, e, "cross-device")
	c := listed(t, e, path)
	e.Rename = func(string, string) error { return syscall.EXDEV }
	if _, err := Apply(context.Background(), e, c, ApplyOpts{}); !errors.Is(err, syscall.EXDEV) {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("source lost", err)
	}
}
func TestHistoryReportOnly(t *testing.T) {
	e, _, _, _ := fixture(t)
	path := filepath.Join(e.Home, ".claude/projects")
	put(t, filepath.Join(path, "history"), "keep")
	c := listed(t, e, path)
	if c.Status != "report_only" {
		t.Fatal(c)
	}
	if _, err := Apply(context.Background(), e, c, ApplyOpts{}); err == nil {
		t.Fatal("deleted history")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	old := e.Now.Add(-60 * 24 * time.Hour)
	if err := os.Chtimes(filepath.Join(path, "history"), old, old); err != nil {
		t.Fatal(err)
	}
	e.Sizes = true
	c = listed(t, e, path)
	if !strings.Contains(strings.Join(c.Evidence, " "), "30d+: 4 bytes") {
		t.Fatal("missing history age breakdown", c)
	}
}
func TestSizeWalkCapped(t *testing.T) {
	root := t.TempDir()
	put(t, filepath.Join(root, "file"), "abc")
	outside := t.TempDir()
	put(t, filepath.Join(outside, "large"), strings.Repeat("x", 1000))
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	size, capped, err := WalkSize(context.Background(), root)
	if err != nil || capped || size != 3 {
		t.Fatal(size, capped, err)
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	_, capped, err = WalkSize(ctx, root)
	if err != nil || !capped {
		t.Fatal(capped, err)
	}
	if sameDevice(&syscall.Stat_t{Dev: 1}, &syscall.Stat_t{Dev: 2}) {
		t.Fatal("walk can cross devices")
	}
	put(t, filepath.Join(root, "mounted", "other-device"), strings.Repeat("x", 100))
	size, capped, err = walkSize(context.Background(), root, func(info os.FileInfo) (uint64, bool) {
		if info.Name() == "mounted" || info.Name() == "other-device" {
			return 2, true
		}
		return 1, true
	})
	if err != nil || capped || size != 3 {
		t.Fatal("walk crossed injected device boundary", size, capped, err)
	}
}

// Real machines run root and other users' daemons whose cwd lsof cannot read.
// Only the user's own processes can be inspected, so only they gate cleanup.
func TestListIgnoresOtherUsersProcesses(t *testing.T) {
	e, p, _, wt := fixture(t)
	p.alive = true
	p.procs = []model.Proc{
		{PID: 200, OwnerUID: uint32(os.Getuid()) + 1, OwnerKnown: true},
		{PID: 201, OwnerUID: uint32(os.Getuid()), OwnerKnown: true, Cwd: "/"},
	}
	cs, err := List(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(p.pids, []int32{201}) {
		t.Fatalf("cwd lookup pids = %v, want only the user's own [201]", p.pids)
	}
	found := false
	for _, c := range cs {
		found = found || c.Path == wt
	}
	if !found {
		t.Fatalf("worktree %s missing from %+v", wt, cs)
	}
	// An own agent whose cwd cannot be read still blocks listing.
	p.procs = append(p.procs, model.Proc{PID: 202, Cmdline: "claude", OwnerUID: uint32(os.Getuid()), OwnerKnown: true})
	if _, err := List(context.Background(), e); err == nil {
		t.Fatal("unreadable own process cwd must fail closed")
	}
}

func TestListUnreadableCwd(t *testing.T) {
	// An empty command line means the process could not be identified (macOS
	// keeps own processes whose argv was unreadable), so it fails closed too.
	for _, cmd := range []string{"ssh-agent", "gpg-agent", "claude", "npx some-mcp-server", ""} {
		t.Run(cmd, func(t *testing.T) {
			e, p, _, wt := fixture(t)
			p.alive = true
			p.procs = []model.Proc{{PID: 202, Cmdline: cmd}}
			cs, err := List(context.Background(), e)
			if cmd == "claude" || cmd == "npx some-mcp-server" || cmd == "" {
				if err == nil || !strings.Contains(err.Error(), "cannot inspect cwd for live process 202") {
					t.Fatalf("unreadable agent cwd must fail closed: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, c := range cs {
				if c.Kind == Worktree && !slices.Contains(c.Evidence, "1 processes with unreadable cwd were not checked") {
					t.Fatalf("missing unreadable cwd evidence: %+v", c)
				}
				found = found || c.Path == wt
			}
			if !found {
				t.Fatal("worktree candidate missing")
			}
		})
	}
}

// Driga judges "merged" against the default branch; git branch -d judges the
// upstream. When they disagree the worktree is already gone, so the branch is
// kept with a warning rather than reporting the whole action as failed.
func TestApplyWorktreeBranchKeptWhenGitRefuses(t *testing.T) {
	e, _, repo, wt := fixture(t)
	git(t, repo, "branch", "old", "HEAD")
	git(t, wt, "commit", "--allow-empty", "-m", "merged work")
	git(t, repo, "merge", "--ff-only", "topic")
	git(t, wt, "config", "branch.topic.remote", ".")
	git(t, wt, "config", "branch.topic.merge", "refs/heads/old")
	ageIndex(t, e, wt)
	c := listed(t, e, wt)
	r, err := Apply(context.Background(), e, c, ApplyOpts{})
	if err != nil {
		t.Fatalf("removal succeeded but Apply failed: %v", err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatal("worktree not removed", err)
	}
	if git(t, repo, "branch", "--list", "topic") == "" {
		t.Fatal("branch deleted although git refused")
	}
	if len(r.Warnings) == 0 {
		t.Fatalf("no warning for kept branch: %+v", r)
	}
	if strings.Contains(git(t, repo, "worktree", "list", "--porcelain"), wt) {
		t.Fatal("worktree prune did not run")
	}
}

func TestCompletedRemovalSurvivesPruneFailure(t *testing.T) {
	e, _, _, wt := fixture(t)
	c := listed(t, e, wt)
	e.Git = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		if len(args) > 1 && args[0] == "worktree" && args[1] == "prune" {
			return nil, fmt.Errorf("prune unavailable")
		}
		return runGit(ctx, dir, args...)
	}
	r, err := Apply(context.Background(), e, c, ApplyOpts{})
	if err != nil || len(r.Warnings) == 0 || !strings.Contains(strings.Join(r.Warnings, " "), "prune") {
		t.Fatalf("completed removal lost: %+v %v", r, err)
	}
	if _, err = os.Stat(wt); !os.IsNotExist(err) {
		t.Fatal("worktree still exists", err)
	}
}

// The audit sink is checked before any removal. A later audit write failure
// must not report a completed removal as failed; it becomes a warning.
func TestApplyAuditWriteFailureIsWarning(t *testing.T) {
	e, _, _, wt := fixture(t)
	ageIndex(t, e, wt)
	c := listed(t, e, wt)
	// A full log needs rotation into a new .1 file, which a read-only data
	// directory refuses, while the existing log itself still opens for append.
	if err := os.MkdirAll(e.DataDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.DataDir, "cleanup.log"), make([]byte, logfile.MaxSize), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(e.DataDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(e.DataDir, 0700) })
	r, err := Apply(context.Background(), e, c, ApplyOpts{})
	if err != nil {
		t.Fatalf("removal succeeded but Apply failed: %v", err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatal("worktree not removed", err)
	}
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "audit log") {
		t.Fatalf("warnings: %q", r.Warnings)
	}
}

// With Why, a worktree that does not qualify is listed with its refusal
// reason, and it still cannot be applied.
func TestWhyListsRefusalReason(t *testing.T) {
	e, _, _, wt := fixture(t)
	put(t, filepath.Join(wt, "tracked"), "change")
	absent(t, e, wt)
	e.Why = true
	c := listed(t, e, wt)
	if c.Status != "refused" || len(c.Evidence) != 1 || c.Evidence[0] != "worktree has tracked changes" {
		t.Fatalf("refused candidate: %+v", c)
	}
	if _, err := Apply(context.Background(), e, c, ApplyOpts{}); err == nil || !strings.Contains(err.Error(), "tracked changes") {
		t.Fatalf("refused candidate applied: %v", err)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatal("worktree touched", err)
	}
}

// Claude Code names project dirs with replace(/[^a-zA-Z0-9]/g, "-"), which
// turns underscores into dashes as well.
func TestProjectSlugMatchesClaude(t *testing.T) {
	if got := projectSlug("/Users/me/my_app.v2"); got != "-Users-me-my-app-v2" {
		t.Fatal(got)
	}
}

func TestWhyOmitsMainWorktree(t *testing.T) {
	e, _, repo, _ := fixture(t)
	e.Why = true
	cs, err := List(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		if c.Path == repo {
			t.Fatalf("main worktree listed: %+v", c)
		}
	}
}
