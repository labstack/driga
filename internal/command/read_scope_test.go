package command

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadRequiresCanonicalScopedPaths(t *testing.T) {
	s := Scope{Project: t.TempDir(), Scratch: t.TempDir()}
	outside := t.TempDir()
	for _, path := range []string{filepath.Join(s.Project, "README.md"), filepath.Join(s.Scratch, "notes.txt"), filepath.Join(outside, "notes.txt")} {
		if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(s.Project, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		argv []string
		cwd  string
		want Class
	}{
		{[]string{"cat", "README.md"}, s.Project, Read},
		{[]string{"cat", filepath.Join(s.Scratch, "notes.txt")}, s.Project, Read},
		{[]string{"cat", filepath.Join(outside, "notes.txt")}, s.Project, Other},
		{[]string{"cat", "README.md", filepath.Join(outside, "notes.txt")}, s.Project, Other},
		{[]string{"cat", "escape/notes.txt"}, s.Project, Other},
		{[]string{"cat", "missing"}, s.Project, Other},
		{[]string{"cat", "~/notes.txt"}, s.Project, Other},
		{[]string{"ls"}, outside, Other},
		{[]string{"ps", "-ef"}, s.Project, Read},
		{[]string{"df", "-h"}, s.Project, Read},
		{[]string{"uptime"}, s.Project, Read},
		{[]string{"grep", "-e", "not/a/path", "README.md"}, s.Project, Read},
		{[]string{"grep", "needle", filepath.Join(outside, "notes.txt")}, s.Project, Other},
		{[]string{"grep", "-ne", "needle", filepath.Join(outside, "notes.txt")}, s.Project, Other},
		{[]string{"grep", "--", "-", filepath.Join(outside, "notes.txt")}, s.Project, Other},
		{[]string{"head", "-n", "2", "README.md"}, s.Project, Read},
		{[]string{"find", ".", "-name", "not/a/path"}, s.Project, Read},
	} {
		t.Run(tc.cwd+"/"+filepath.Join(tc.argv...), func(t *testing.T) {
			p, err := Parse(Request{Argv: tc.argv, Cwd: tc.cwd})
			if err != nil {
				t.Fatal(err)
			}
			pol, err := Classify(p, s)
			if err != nil {
				t.Fatal(err)
			}
			if pol.Class != tc.want {
				t.Fatalf("got %s, want %s", pol.Class, tc.want)
			}
		})
	}
}
