//go:build !windows

package cleanup

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func buildFixture(t *testing.T) (Env, *fakePlatform, string, string) {
	t.Helper()
	e, p, repo, _ := fixture(t)
	e.Cfg.BuildIdle = 7 * 24 * time.Hour
	repo = resolved(repo)
	path := filepath.Join(repo, "node_modules")
	put(t, filepath.Join(path, "x"), "regenerable")
	return e, p, repo, path
}

func buildRefused(t *testing.T, e Env, path, reason string) {
	t.Helper()
	absent(t, e, path)
	e.Why = true
	c := listed(t, e, path)
	if c.Status != "refused" || !strings.Contains(strings.Join(c.Evidence, ";"), reason) {
		t.Fatalf("want %q: %+v", reason, c)
	}
}

func TestBuildNodeModulesReady(t *testing.T) {
	e, _, _, path := buildFixture(t)
	c := listed(t, e, path)
	if c.Kind != Build || c.Status != "ready" || !strings.Contains(strings.Join(c.Evidence, ";"), "package.json") {
		t.Fatal(c)
	}
}

func TestBuildPycacheAbsent(t *testing.T) {
	e, _, repo, _ := buildFixture(t)
	put(t, filepath.Join(repo, ".gitignore"), "node_modules/\n__pycache__/\n")
	path := filepath.Join(repo, "__pycache__")
	put(t, filepath.Join(path, "module.pyc"), "bytecode")
	absent(t, e, path)
	e.Why = true
	absent(t, e, path)
}

func TestBuildRootCwdDoesNotBlock(t *testing.T) {
	e, p, repo, path := buildFixture(t)
	p.procs = []model.Proc{{PID: 999, Cwd: repo, Cmdline: "zsh"}}
	if c := listed(t, e, path); c.Status != "ready" {
		t.Fatal(c)
	}
	p.procs[0].Cwd = path
	buildRefused(t, e, path, "process cwd is inside")
}

func TestBuildOpenFileBlocks(t *testing.T) {
	e, p, _, path := buildFixture(t)
	p.openPaths = []string{filepath.Join(path, "x")}
	buildRefused(t, e, path, "process has an open file inside")
}

func TestBuildNestedGitRefused(t *testing.T) {
	for _, rel := range []string{"dep/.git", "@scope/dep/.git"} {
		t.Run(rel, func(t *testing.T) {
			e, _, _, path := buildFixture(t)
			put(t, filepath.Join(path, rel, "HEAD"), "ref: refs/heads/main")
			buildRefused(t, e, path, "contains a nested git repository")
		})
	}
}

func TestBuildDeepTreeListed(t *testing.T) {
	e, _, _, path := buildFixture(t)
	put(t, filepath.Join(path, "a/b/c/d/e/.git/HEAD"), "beyond inspection depth")
	if c := listed(t, e, path); c.Status != "ready" {
		t.Fatal(c)
	}
}

func TestBuildVerifyRereadsCheckout(t *testing.T) {
	for _, change := range []string{"ignored paths", "last commit"} {
		t.Run(change, func(t *testing.T) {
			e, _, repo, path := buildFixture(t)
			c := listed(t, e, path)
			reason := "not an ignored directory"
			if change == "ignored paths" {
				put(t, filepath.Join(repo, ".gitignore"), "")
			} else {
				cmd := exec.Command("git", "-C", repo, "commit", "--allow-empty", "-m", "recent")
				date := e.Now.Add(-time.Hour).Format(time.RFC3339)
				cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("%s %v", out, err)
				}
				reason = "checkout active within cleanup.build_idle"
			}
			if err := Verify(context.Background(), e, c); err == nil || !strings.Contains(err.Error(), reason) {
				t.Fatalf("want %q, got %v", reason, err)
			}
		})
	}
}

func TestBuildRecentCommitRefused(t *testing.T) {
	e, _, repo, path := buildFixture(t)
	cmd := exec.Command("git", "-C", repo, "commit", "--allow-empty", "-m", "recent")
	date := e.Now.Add(-time.Hour).Format(time.RFC3339)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v", out, err)
	}
	buildRefused(t, e, path, "checkout active within cleanup.build_idle")
}

func TestBuildUnbornRepoRefused(t *testing.T) {
	e, _, _, _ := fixture(t)
	repo := t.TempDir()
	git(t, repo, "init")
	put(t, filepath.Join(repo, "package.json"), "{}")
	put(t, filepath.Join(repo, ".gitignore"), "node_modules/\n")
	repo = resolved(repo)
	path := filepath.Join(repo, "node_modules")
	put(t, filepath.Join(path, "x"), "cache")
	tx(t, e, func(tx *store.Tx) error { _, err := tx.UpsertRepo(model.Repo{Path: repo}); return err })
	buildRefused(t, e, path, "checkout has no commits")
}

func TestBuildSiblingDiscovered(t *testing.T) {
	for _, rel := range []string{"sibling", ".hidden", "group/nested"} {
		t.Run(rel, func(t *testing.T) {
			e, _, repo, _ := buildFixture(t)
			sibling := filepath.Join(filepath.Dir(repo), rel)
			if err := os.MkdirAll(sibling, 0700); err != nil {
				t.Fatal(err)
			}
			git(t, sibling, "init", "-b", "main")
			git(t, sibling, "config", "user.email", "test@example.invalid")
			git(t, sibling, "config", "user.name", "Test")
			put(t, filepath.Join(sibling, "package.json"), "{}")
			put(t, filepath.Join(sibling, ".gitignore"), "node_modules/\n")
			git(t, sibling, "add", ".")
			git(t, sibling, "commit", "-m", "base")
			path := filepath.Join(sibling, "node_modules")
			put(t, filepath.Join(path, "x"), "cache")
			if c := listed(t, e, path); c.Kind != Build {
				t.Fatal(c)
			}
		})
	}
}

func TestBuildTrackedLookalikeAbsent(t *testing.T) {
	e, _, repo, _ := buildFixture(t)
	put(t, filepath.Join(repo, "internal", "Cargo.toml"), "")
	path := filepath.Join(repo, "internal", "target")
	put(t, filepath.Join(path, "x.go"), "package target")
	git(t, repo, "add", "internal")
	git(t, repo, "commit", "-m", "source")
	absent(t, e, path)
}

func TestBuildListDoesNotProbePerCandidate(t *testing.T) {
	e, p, repo, _ := buildFixture(t)
	put(t, filepath.Join(repo, ".gitignore"), "node_modules/\n.next/\ndist/\n")
	put(t, filepath.Join(repo, ".next", "x"), "cache")
	put(t, filepath.Join(repo, "dist", "x"), "cache")
	statusCalls, logCalls := 0, 0
	e.Git = func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		if dir == repo {
			if args[0] == "status" {
				statusCalls++
			}
			if args[0] == "log" {
				logCalls++
			}
		}
		return runGit(ctx, dir, args...)
	}
	cs, err := List(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, c := range cs {
		if c.Kind == Build {
			n++
		}
	}
	if n != 3 || p.openCalls != 0 || p.openPathCalls != 1 {
		t.Fatalf("builds=%d probes=%d tables=%d", n, p.openCalls, p.openPathCalls)
	}
	if statusCalls != 1 || logCalls != 1 {
		t.Fatalf("status calls=%d log calls=%d", statusCalls, logCalls)
	}
}

func TestBuildRemoveDeletes(t *testing.T) {
	e, _, repo, path := buildFixture(t)
	c := listed(t, e, path)
	r, err := Apply(context.Background(), e, c, ApplyOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("directory survived", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "tracked")); err != nil {
		t.Fatal(err)
	}
	if r.TrashPath != "" {
		t.Fatal(r)
	}
	b, err := os.ReadFile(filepath.Join(e.DataDir, "cleanup.log"))
	if err != nil {
		t.Fatal(err)
	}
	var entry struct {
		Result Result `json:"result"`
	}
	if err := json.Unmarshal(b, &entry); err != nil {
		t.Fatal(err)
	}
	if len(entry.Result.Commands) != 1 || entry.Result.Commands[0][0] != "remove" {
		t.Fatal(string(b))
	}
	for _, p := range []string{filepath.Join(e.Home, ".Trash"), filepath.Join(e.Home, ".local/share/Trash")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatal("Trash created", p, err)
		}
	}
}

func TestBuildSymlinkSwapRefused(t *testing.T) {
	e, _, _, path := buildFixture(t)
	c := listed(t, e, path)
	outside := t.TempDir()
	put(t, filepath.Join(outside, "sentinel"), "keep")
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(context.Background(), e, c, ApplyOpts{}); err == nil {
		t.Fatal("symlink accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "sentinel")); err != nil {
		t.Fatal(err)
	}
}

func TestBuildDryRun(t *testing.T) {
	e, _, _, path := buildFixture(t)
	r, err := Apply(context.Background(), e, listed(t, e, path), ApplyOpts{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Commands) != 1 || r.Commands[0][0] != "remove" {
		t.Fatal(r)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestBuildVenvPackagesLogged(t *testing.T) {
	e, _, repo, _ := buildFixture(t)
	put(t, filepath.Join(repo, "pyproject.toml"), "")
	put(t, filepath.Join(repo, ".gitignore"), ".venv/\nnode_modules/\n")
	path := filepath.Join(repo, ".venv")
	put(t, filepath.Join(path, "lib/python3.13/site-packages/requests-2.32.0.dist-info/METADATA"), "")
	r, err := Apply(context.Background(), e, listed(t, e, path), ApplyOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Packages) != 1 || r.Packages[0] != "requests-2.32.0" {
		t.Fatal(r)
	}
	b, err := os.ReadFile(filepath.Join(e.DataDir, "cleanup.log"))
	if err != nil || !strings.Contains(string(b), `"packages":["requests-2.32.0"]`) {
		t.Fatalf("%s %v", b, err)
	}
}

func TestBuildForceAddedContentAbsent(t *testing.T) {
	e, _, repo, path := buildFixture(t)
	git(t, repo, "add", "-f", "node_modules/x")
	git(t, repo, "commit", "-m", "keep source")
	e.Why = true
	absent(t, e, path)
}
