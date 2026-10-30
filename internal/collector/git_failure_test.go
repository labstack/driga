package collector

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/gitscan"
)

func TestGitWorktreeFailuresAreIsolated(t *testing.T) {
	for _, kind := range []string{"deleted", "no-marker", "corrupt"} {
		t.Run(kind, func(t *testing.T) {
			d, st, plat, a := setup(t)
			a.obs = nil
			plat.procs = nil
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
			for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR"} {
				t.Setenv(key, "")
				if err := os.Unsetenv(key); err != nil {
					t.Fatal(err)
				}
			}
			repo := t.TempDir()
			linked := filepath.Join(t.TempDir(), "linked")
			git := func(args ...string) {
				t.Helper()
				c := exec.Command("git", args...)
				c.Dir = repo
				if out, err := c.CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v: %s", args, err, out)
				}
			}
			git("init", "-b", "main")
			git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "initial")
			git("worktree", "add", "-b", "topic", linked)
			d.Cfg.Roots = []string{repo}
			pass(t, d, st, Plan{Git: true, GitFull: true})
			marker := filepath.Join(linked, ".git")
			original, err := os.ReadFile(marker)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "deleted":
				err = os.RemoveAll(linked)
			case "no-marker":
				err = os.Remove(marker)
			case "corrupt":
				err = os.WriteFile(marker, []byte("gitdir: missing-gitdir\n"), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "healthy update")
			var logs bytes.Buffer
			old := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			defer slog.SetDefault(old)
			at := d.Now().Add(time.Hour)
			d.Now = func() time.Time { return at }
			for i := 0; i < 2; i++ {
				// A fresh scanner also proves diagnostics survive service restarts.
				d.Git = gitscan.NewScanner(2)
				r, err := Pass(context.Background(), d, st, Plan{Git: true, GitFull: true})
				if err != nil || r.SourceErrors["git"] != "" {
					t.Fatalf("git source failed: %v %+v", err, r.SourceErrors)
				}
				ws, err := st.Worktrees(0)
				if err != nil {
					t.Fatal(err)
				}
				want := 1
				if kind == "corrupt" {
					want = 2
				}
				if len(ws) != want {
					t.Fatalf("inventory: %+v", ws)
				}
				for _, w := range ws {
					if w.Path == gitscan.NormalizePath(repo) && w.LastCommitSubject != "healthy update" {
						t.Fatalf("healthy checkout stale: %+v", w)
					}
					if w.Path == gitscan.NormalizePath(linked) {
						if w.ScanError == "" || w.LastCommitSubject != "initial" {
							t.Fatalf("missing failure or lost previous observation: %+v", w)
						}
					}
				}
				sources, err := st.Sources()
				if err != nil {
					t.Fatal(err)
				}
				for _, s := range sources {
					if s.Name == "git" && !s.LastOKAt.Equal(at) {
						t.Fatalf("freshness: %+v", s)
					}
				}
				candidates, err := cleanup.List(context.Background(), cleanup.Env{Store: st, Plat: plat, Sigs: d.Sigs, Home: os.Getenv("HOME"), DataDir: t.TempDir(), Now: at, Cfg: d.Cfg.Cleanup, Why: true, TempRoots: []string{t.TempDir()}})
				if err != nil {
					t.Fatal(err)
				}
				for _, c := range candidates {
					if kind != "corrupt" && c.Path == gitscan.NormalizePath(linked) {
						t.Fatalf("retired cleanup candidate: %+v", c)
					}
				}
			}
			count := strings.Count(logs.String(), "git_worktree_failed")
			want := 0
			if kind == "corrupt" {
				want = 1
			}
			if count != want {
				t.Fatalf("failure logs=%d want=%d: %s", count, want, logs.String())
			}
			if kind == "corrupt" {
				if err := os.WriteFile(marker, original, 0600); err != nil {
					t.Fatal(err)
				}
				pass(t, d, st, Plan{Git: true, GitFull: true})
				ws, err := st.Worktrees(0)
				if err != nil {
					t.Fatal(err)
				}
				for _, w := range ws {
					if w.ScanError != "" {
						t.Fatalf("recovery retained error: %+v", w)
					}
				}
				// A new diagnostic logs once; recurrence of a previously seen one
				// stays quiet even after recovery.
				for _, contents := range []string{"invalid gitfile\n", "invalid gitfile\n", "gitdir: missing-gitdir\n"} {
					if err := os.WriteFile(marker, []byte(contents), 0600); err != nil {
						t.Fatal(err)
					}
					pass(t, d, st, Plan{Git: true, GitFull: true})
				}
				if count := strings.Count(logs.String(), "git_worktree_failed"); count != 2 {
					t.Fatalf("distinct failure logs=%d: %s", count, logs.String())
				}
			}
		})
	}
}
