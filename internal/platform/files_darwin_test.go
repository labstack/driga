package platform

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestDarwinOpenFilePathsEscapedNames(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"new\nline", "tab\tname", `back\slash`, `literal\nname`, "del\x7fname"}
	paths := []string{}
	for _, name := range names {
		path := filepath.Join(dir, name)
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		paths = append(paths, path)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	got, err := (darwinPlatform{}).OpenFilePaths(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if !slices.Contains(got, path) {
			t.Errorf("open escaped pathname omitted: %q", path)
		}
	}
}

func TestDecodeLsofName(t *testing.T) {
	for _, tc := range []struct{ encoded, want string }{
		{`/normal/space name`, "/normal/space name"},
		{`/new\nline`, "/new\nline"},
		{`/tab\tname`, "/tab\tname"},
		{`/back\\slash`, `/back\slash`},
		{`/literal\\nname`, `/literal\nname`},
		{`/del\x7fname`, "/del\x7fname"},
		{`/other\b\f\r`, "/other\b\f\r"},
		{`/utf-\xc3\xa9`, "/utf-é"},
	} {
		got, err := decodeLsofName(tc.encoded)
		if err != nil || got != tc.want {
			t.Errorf("%q: got %q, %v; want %q", tc.encoded, got, err, tc.want)
		}
	}
	for _, name := range []string{`/truncated\`, `/bad\q`, `/hex\x1`, `/hex\xZZ`, `/null\x00`, "/raw\x00", `/ambiguous^A`, `/ambiguous^[`, `/ambiguous^?`} {
		if _, err := decodeLsofName(name); err == nil {
			t.Errorf("invalid or ambiguous name accepted: %q", name)
		}
	}
}

func TestLsofOpenPathsRetainsVanishedNameAndRejectsUncertainEvidence(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "vanished", "artifact")
	paths, err := lsofOpenPaths([]byte("p123\nn" + missing + "\nnlocalhost:443\n"))
	if err != nil || !slices.Equal(paths, []string{missing}) {
		t.Fatal(paths, err)
	}
	for _, suffix := range []string{`/bad\q`, `/ambiguous^A`} {
		if _, err := lsofOpenPaths([]byte("n" + dir + suffix + "\n")); err == nil {
			t.Fatal("uncertain entry silently accepted", suffix)
		}
	}
	loop := filepath.Join(dir, "loop")
	if err := os.Symlink(loop, loop); err != nil {
		t.Fatal(err)
	}
	if _, err := lsofOpenPaths([]byte("n" + loop + "/artifact\n")); err == nil {
		t.Fatal("unresolvable entry silently accepted")
	}
}

func TestDarwinOpenFilePathsAmbiguousNameRefuses(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "control\x01name"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := (darwinPlatform{}).OpenFilePaths(ctx); err == nil {
		t.Fatal("ambiguous native name accepted")
	}
}

func TestLsofOpenPathsCanonicalizesUnavailableSuffix(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	actual := filepath.Join(dir, "actual")
	if err := os.Mkdir(actual, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias")
	if err := os.Symlink(actual, alias); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(alias, "missing", "artifact")
	paths, err := lsofOpenPaths([]byte("n" + missing + "\n"))
	if err != nil || !slices.Equal(paths, []string{filepath.Join(actual, "missing", "artifact")}) {
		t.Fatal(paths, err)
	}
	restricted := filepath.Join(actual, "restricted")
	if err := os.Mkdir(restricted, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(restricted, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(restricted, 0700)
	paths, err = lsofOpenPaths([]byte("n" + filepath.Join(alias, "restricted", "artifact") + "\n"))
	if err != nil || !slices.Equal(paths, []string{filepath.Join(actual, "restricted", "artifact")}) {
		t.Fatal("unreadable path lost", paths, err)
	}
}
