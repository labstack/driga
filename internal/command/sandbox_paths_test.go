package command

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestWorktreeMetadataProtection(t *testing.T) {
	s := Scope{Project: t.TempDir(), Scratch: t.TempDir()}
	common, _ := filepath.EvalSymlinks(t.TempDir())
	admin := filepath.Join(common, "worktrees", "selected")
	other := filepath.Join(common, "worktrees", "other")
	for _, dir := range []string{admin, other} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for path, value := range map[string]string{
		filepath.Join(s.Project, ".git"):  "gitdir: " + admin + "\n",
		filepath.Join(admin, "commondir"): "../..\n",
		filepath.Join(other, "commondir"): "../..\n",
		filepath.Join(admin, "HEAD"):      "ref: refs/heads/selected\n",
		filepath.Join(other, "HEAD"):      "ref: refs/heads/other\n",
		filepath.Join(common, "HEAD"):     "ref: refs/heads/main\n",
	} {
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"objects", "refs"} {
		if err := os.Mkdir(filepath.Join(common, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	w, err := sandboxWrites(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{common, admin, other} {
		if !slices.ContainsFunc(w.Roots, func(parent string) bool { return within(root, parent) }) {
			t.Errorf("git metadata root not writable: %s", root)
		}
		for _, name := range []string{"hooks", "config", "config.worktree", "info/attributes", "commondir", "gitdir", ".envrc", ".gitattributes", ".vscode", ".idea", ".husky"} {
			if !slices.Contains(w.Protected, protectedWrite{Path: filepath.Join(root, name)}) {
				t.Errorf("worktree metadata not protected: %s/%s", root, name)
			}
		}
		for _, state := range []string{"objects", "refs", "index", "HEAD", "logs"} {
			for _, path := range w.Protected {
				if !path.Literal && within(filepath.Join(root, state), path.Path) {
					t.Errorf("ordinary Git state protected: %s", state)
				}
			}
		}
	}
	project, _ := filepath.EvalSymlinks(s.Project)
	if !slices.Contains(w.Protected, protectedWrite{Path: filepath.Join(project, ".git")}) {
		t.Fatal("worktree .git pointer writable")
	}
}

func TestBubblewrapPlaceholderCleanup(t *testing.T) {
	s := Scope{Project: t.TempDir(), Scratch: t.TempDir()}
	if err := os.MkdirAll(filepath.Join(s.Project, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	args, err := bubbleArgs(Prepared{Scope: s})
	if err != nil {
		t.Fatal(err)
	}
	cleanup, err := bubblePlaceholders(args)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if b, err := os.ReadFile(filepath.Join(s.Project, ".git", "commondir")); err != nil || string(b) != ".\n" {
		t.Fatalf("Git-compatible commondir placeholder: %q %v", b, err)
	}
	cleanup()
	for _, path := range []string{filepath.Join(s.Project, ".git", "commondir"), filepath.Join(s.Project, ".envrc"), filepath.Join(s.Scratch, ".git")} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("placeholder left behind: %s (%v)", path, err)
		}
	}
}

func TestUntrustedGitPointersCannotExpandWriteScope(t *testing.T) {
	s := Scope{Project: t.TempDir(), Scratch: t.TempDir()}
	nested := filepath.Join(s.Project, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, ".git"), []byte("gitdir: "+t.TempDir()+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := sandboxWrites(s); err == nil {
		t.Fatal("a nested gitdir pointer granted an unrelated external write root")
	}
}

func TestSelectedGitPointerMustIdentifyMetadata(t *testing.T) {
	s := Scope{Project: t.TempDir(), Scratch: t.TempDir()}
	if err := os.WriteFile(filepath.Join(s.Project, ".git"), []byte("gitdir: "+t.TempDir()+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := sandboxWrites(s); err == nil {
		t.Fatal("selected .git pointer granted a non-repository external directory")
	}
}

func TestProtectedSymlinksFailClosed(t *testing.T) {
	for _, name := range []string{".envrc", ".git", ".git/info"} {
		t.Run(name, func(t *testing.T) {
			s := Scope{Project: t.TempDir(), Scratch: t.TempDir()}
			path := filepath.Join(s.Project, name)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(t.TempDir(), path); err != nil {
				t.Fatal(err)
			}
			if _, err := sandboxWrites(s); err == nil {
				t.Fatal("replaceable protected symlink accepted")
			}
		})
	}
}
