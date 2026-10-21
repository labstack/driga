package codex

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTemporaryCwdViaSymlinkedTempRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix symlink fixture; Windows watch tests require no Developer Mode or admin rights")
	}

	// Use a custom OS temp root nested in the test's own temp dir (never the
	// source tree). On macOS that is outside /tmp, so the built-in /tmp
	// fallback cannot hide a failure to resolve the custom root.
	workdir := filepath.Join(t.TempDir(), "custom-tmp")
	if err := os.Mkdir(workdir, 0o700); err != nil {
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
			if !tc.want && (base == "/tmp" || strings.HasPrefix(base, "/tmp/")) {
				t.Skip("negative case needs a base outside the built-in /tmp root (Linux test temp dirs live in /tmp)")
			}
			t.Setenv("TMPDIR", tc.tempRoot)
			if got := temporary(tc.cwd); got != tc.want {
				t.Fatalf("temporary(%q)=%v, want %v (temp root %q)", tc.cwd, got, tc.want, tc.tempRoot)
			}
		})
	}
}

func TestLinuxTempCwdVarTmp(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	for _, cwd := range []string{"/var/tmp", "/var/tmp/driga-synthetic/session"} {
		if !temporary(cwd) {
			t.Fatalf("%s must be temporary", cwd)
		}
	}
	if temporary("/var/tmp-other/session") {
		t.Fatal("path component boundary")
	}
}
