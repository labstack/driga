package command

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestSeatbeltProtectsExecutionHooks(t *testing.T) {
	s := Scope{Project: t.TempDir(), Scratch: t.TempDir()}
	s.Project, _ = filepath.EvalSymlinks(s.Project)
	s.Scratch, _ = filepath.EvalSymlinks(s.Scratch)
	if err := os.MkdirAll(filepath.Join(s.Project, ".git", "info"), 0700); err != nil {
		t.Fatal(err)
	}
	p := Prepared{Scope: s}
	profile, err := sandboxProfile(p)
	if err != nil {
		t.Fatal(err)
	}
	lastAllow := strings.LastIndex(profile, "(allow file-write")
	for _, root := range []string{s.Project, s.Scratch} {
		for _, name := range []string{".git/hooks", ".git/config", ".git/config.worktree", ".git/info/attributes", ".gitattributes", ".envrc", ".vscode", ".idea", ".husky"} {
			if root == s.Scratch && strings.HasPrefix(name, ".git/") {
				name = ".git"
			}
			rule := "(deny file-write* (subpath " + strconv.Quote(filepath.Join(root, name)) + "))"
			if i := strings.Index(profile, rule); i < lastAllow || i < 0 {
				t.Errorf("missing late protection: %s", rule)
			}
		}
	}
}
