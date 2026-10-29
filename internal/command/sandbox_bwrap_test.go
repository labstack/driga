package command

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestBubblewrapProtectsExecutionHooks(t *testing.T) {
	s := Scope{Project: t.TempDir(), Scratch: t.TempDir()}
	s.Project, _ = filepath.EvalSymlinks(s.Project)
	s.Scratch, _ = filepath.EvalSymlinks(s.Scratch)
	for _, name := range []string{".git/hooks", ".git/info", ".vscode"} {
		if err := os.MkdirAll(filepath.Join(s.Project, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{".git/config", ".git/info/attributes", ".envrc"} {
		if err := os.WriteFile(filepath.Join(s.Project, name), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	args, err := bubbleArgs(Prepared{Scope: s})
	if err != nil {
		t.Fatal(err)
	}
	bind := slices.Index(args, "--bind")
	for _, name := range []string{".git/hooks", ".git/config", ".git/info/attributes", ".envrc", ".vscode"} {
		path := filepath.Join(s.Project, name)
		found := false
		for i := bind + 1; i+2 < len(args); i++ {
			if args[i] == "--ro-bind" && args[i+1] == path && args[i+2] == path {
				found = true
			}
		}
		if !found {
			t.Errorf("no read-only rebind for %s: %v", name, args)
		}
	}
	for _, name := range []string{".gitattributes", ".idea", ".husky"} {
		if !slices.Contains(args, filepath.Join(s.Project, name)) {
			t.Errorf("absent %s can be created", name)
		}
	}
	if !slices.Contains(args, filepath.Join(s.Scratch, ".envrc")) {
		t.Error("scratch has no persistence protection")
	}
}

func TestBubblewrapRequiresUserNamespace(t *testing.T) {
	args, err := bubbleArgs(Prepared{Scope: Scope{Scratch: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	// In 0.9, --unshare-all only enables --unshare-user-try. The parser
	// rejects --disable-userns unless --unshare-user is explicitly present.
	for _, flag := range []string{"--unshare-all", "--unshare-user", "--disable-userns", "--new-session", "--die-with-parent", "--cap-drop", "ALL"} {
		if !slices.Contains(args, flag) {
			t.Errorf("missing %s: %v", flag, args)
		}
	}
}
