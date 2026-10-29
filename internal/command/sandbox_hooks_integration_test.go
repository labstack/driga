package command

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "core.hooksPath=/dev/null"}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, b)
	}
	return strings.TrimSpace(string(b))
}

func testSandboxExecutionHooks(t *testing.T) {
	t.Helper()
	for _, layout := range []string{"project", "nested", "worktree"} {
		for _, present := range []bool{false, true} {
			t.Run(layout+map[bool]string{false: "/absent", true: "/existing"}[present], func(t *testing.T) {
				s := runnerScope(t)
				cwd := s.Project
				if layout == "nested" {
					cwd = filepath.Join(s.Project, "nested")
					if err := os.Mkdir(cwd, 0700); err != nil {
						t.Fatal(err)
					}
				}
				var metadata []string
				if layout == "worktree" {
					main := t.TempDir()
					fixtureGit(t, main, "init", "-q")
					fixtureGit(t, main, "commit", "--allow-empty", "-qm", "initial")
					fixtureGit(t, main, "worktree", "add", "-b", "selected", cwd)
					other := filepath.Join(t.TempDir(), "other")
					fixtureGit(t, main, "worktree", "add", "-b", "other", other)
					metadata = append(metadata, fixtureGit(t, main, "rev-parse", "--absolute-git-dir"), fixtureGit(t, other, "rev-parse", "--absolute-git-dir"))
				} else {
					fixtureGit(t, cwd, "init", "-q")
				}
				metadata = append(metadata, fixtureGit(t, cwd, "rev-parse", "--absolute-git-dir"))
				var targets []string
				for _, root := range []string{s.Project, s.Scratch, cwd} {
					if st, err := os.Stat(filepath.Join(root, ".git")); os.IsNotExist(err) || err == nil && st.IsDir() {
						for _, name := range []string{".git/hooks/pre-commit", ".git/config", ".git/config.worktree", ".git/info/attributes"} {
							targets = append(targets, filepath.Join(root, name))
						}
					}
					for _, name := range []string{".gitattributes", ".envrc", ".vscode/settings.json", ".idea/workspace.xml", ".husky/pre-commit"} {
						targets = append(targets, filepath.Join(root, name))
					}
				}
				for _, root := range metadata {
					for _, name := range []string{"hooks/pre-commit", "config", "config.worktree", "info/attributes", "commondir", "gitdir", ".gitattributes", ".envrc", ".vscode/settings.json", ".idea/workspace.xml", ".husky/pre-commit"} {
						targets = append(targets, filepath.Join(root, name))
					}
				}
				if layout == "worktree" {
					targets = append(targets, filepath.Join(cwd, ".git"))
				}
				if present {
					for _, path := range targets {
						if _, err := os.Stat(path); err == nil {
							continue
						}
						if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
							t.Fatal(err)
						}
						value := "# safe fixture\n"
						if filepath.Base(path) == "commondir" {
							value = ".\n"
						}
						if err := os.WriteFile(path, []byte(value), 0600); err != nil {
							t.Fatal(err)
						}
					}
				}
				run := func(script string) Result {
					t.Helper()
					p := prepareRun(t, Request{Script: script, Cwd: cwd}, s)
					var out, stderr bytes.Buffer
					r, err := Run(context.Background(), p, &out, &stderr)
					if err != nil {
						t.Fatalf("sandbox launch: %v %s", err, stderr.String())
					}
					return r
				}
				for _, path := range targets {
					before, beforeErr := os.ReadFile(path)
					// Expansion reaches the OS boundary rather than the classifier.
					if r := run("target=" + quote(path) + "; mkdir -p \"$(dirname \"$target\")\" && printf planted > \"$target\""); r.ExitCode == 0 {
						t.Fatalf("persistence write succeeded: %s", path)
					}
					after, afterErr := os.ReadFile(path)
					if !bytes.Equal(before, after) || (beforeErr == nil) != (afterErr == nil) {
						t.Fatalf("host path changed: %s (%v)", path, afterErr)
					}
				}
				for _, path := range []string{filepath.Join(cwd, ".envrc"), filepath.Join(metadata[len(metadata)-1], "config")} {
					if r := run("printf replacement > replacement; target=" + quote(path) + "; mv -f replacement \"$target\""); r.ExitCode == 0 {
						t.Fatalf("atomic replacement succeeded: %s", path)
					}
				}
				if r := run("printf ordinary > ordinary.txt; git -c user.name=Fixture -c user.email=fixture@example.invalid add ordinary.txt && git -c user.name=Fixture -c user.email=fixture@example.invalid commit -qm ordinary"); r.ExitCode != 0 {
					t.Fatalf("ordinary git commit failed: %d", r.ExitCode)
				}
				if got := fixtureGit(t, cwd, "log", "-1", "--format=%s"); got != "ordinary" {
					t.Fatalf("commit missing: %s", got)
				}
			})
		}
	}
}
