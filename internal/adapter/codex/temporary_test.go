package codex

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTemporaryCwdViaSymlinkedTempRoot(t *testing.T) {
	// Keep the fixture outside /tmp so that the built-in /tmp fallback cannot
	// hide a failure to resolve the custom OS temp root.
	workdir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", workdir)
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "temp root")
	child := filepath.Join(root, "session ü")
	sibling := root + "-other"
	for _, dir := range []string{child, sibling} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(base, "alias")
	otherAlias := filepath.Join(base, "other alias")
	for _, link := range []string{alias, otherAlias} {
		if err := os.Symlink(root, link); err != nil {
			t.Fatal(err)
		}
	}
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	nestedAlias := filepath.Join(base, "nested alias")
	if err := os.Symlink(nested, nestedAlias); err != nil {
		t.Fatal(err)
	}
	escape := filepath.Join(root, "escape")
	if err := os.Symlink(sibling, escape); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, tempRoot, cwd string
		want                bool
	}{
		{"root alias", alias, child, true},
		{"cwd alias", root, filepath.Join(alias, "session ü"), true},
		{"both aliases", alias, filepath.Join(otherAlias, "session ü") + "/", true},
		{"component boundary", alias, sibling, false},
		{"symlink parent inside", root, nestedAlias + "/..", true},
		{"symlink parent outside", root, escape + "/..", false},
		{"missing cwd", alias, filepath.Join(root, "missing"), true},
		{"missing cwd via alias", root, filepath.Join(alias, "missing"), true},
		{"missing root", filepath.Join(base, "missing"), filepath.Join(base, "missing", "session"), true},
		{"missing sibling", filepath.Join(base, "missing"), filepath.Join(base, "missing-other"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TMPDIR", tc.tempRoot)
			if got := temporary(tc.cwd); got != tc.want {
				t.Fatalf("temporary(%q)=%v, want %v (temp root %q)", tc.cwd, got, tc.want, tc.tempRoot)
			}
		})
	}
}
