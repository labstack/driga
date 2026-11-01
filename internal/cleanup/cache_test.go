package cleanup

import (
	"context"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/store"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func cacheFixture(t *testing.T) (Env, *[]string, string) {
	t.Helper()
	t.Setenv("BUN_INSTALL_CACHE_DIR", "")
	t.Setenv("BUN_INSTALL", "")
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	e := Env{Store: st, Plat: &platform.Fake{}, Home: t.TempDir(), DataDir: t.TempDir(), Cfg: config.Default().Cleanup, TempRoots: []string{t.TempDir()}}
	path := resolved(t.TempDir())
	calls := []string{}
	e.RunTools = true
	e.Tool = func(_ context.Context, binary string, args ...string) ([]byte, error) {
		command := binary + " " + strings.Join(args, " ")
		calls = append(calls, command)
		if command == "go env GOCACHE" {
			return []byte(path + "\n"), nil
		}
		if command == "go clean -cache" {
			return nil, nil
		}
		return nil, exec.ErrNotFound
	}
	return e, &calls, path
}

func cacheListed(t *testing.T, e Env, id string) Candidate {
	t.Helper()
	cs, err := List(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("missing %s: %+v", id, cs)
	return Candidate{}
}

func TestCacheListsLocatedTool(t *testing.T) {
	e, _, path := cacheFixture(t)
	c := cacheListed(t, e, "cache:go-build")
	if c.Kind != Cache || c.Status != "ready" || c.Path != path {
		t.Fatal(c)
	}
}

func TestCacheBunDefaultPath(t *testing.T) {
	e, calls, _ := cacheFixture(t)
	e.RunTools = false
	path := filepath.Join(e.Home, ".bun", "install", "cache")
	put(t, filepath.Join(path, "package"), "cache")
	c := cacheListed(t, e, "cache:bun")
	if c.Path != path || c.Status != "ready" || !slices.Equal(c.Evidence, []string{"clean with: remove " + path}) {
		t.Fatal(c)
	}
	if len(*calls) != 0 {
		t.Fatal("fixed cache listing ran tools", *calls)
	}
}

func TestCacheBunEnvOverride(t *testing.T) {
	e, _, _ := cacheFixture(t)
	e.RunTools = false
	path := t.TempDir()
	t.Setenv("BUN_INSTALL_CACHE_DIR", path)
	t.Setenv("BUN_INSTALL", t.TempDir())
	if c := cacheListed(t, e, "cache:bun"); c.Path != path {
		t.Fatal(c)
	}
}

func TestCacheBunInstallPath(t *testing.T) {
	e, _, _ := cacheFixture(t)
	e.RunTools = false
	install := t.TempDir()
	t.Setenv("BUN_INSTALL", install)
	path := filepath.Join(install, "install", "cache")
	put(t, filepath.Join(path, "package"), "cache")
	if c := cacheListed(t, e, "cache:bun"); c.Path != path {
		t.Fatal(c)
	}
}

func TestCacheBunAbsentDirectorySkipped(t *testing.T) {
	e, _, _ := cacheFixture(t)
	cs, err := List(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		if c.ID == "cache:bun" {
			t.Fatal(c)
		}
	}
}

func TestCacheBunApplyRemovesDirectory(t *testing.T) {
	e, calls, _ := cacheFixture(t)
	path := filepath.Join(e.Home, ".bun", "install", "cache")
	put(t, filepath.Join(path, "package"), "cache")
	c := cacheListed(t, e, "cache:bun")
	*calls = nil
	e.RunTools = false
	if _, err := Apply(context.Background(), e, c, ApplyOpts{}); err == nil || err.Error() != "cache cleanup runs from the driga CLI only" {
		t.Fatal(err)
	}
	e.RunTools = true
	r, err := Apply(context.Background(), e, c, ApplyOpts{DryRun: true})
	if err != nil || len(r.Commands) != 1 || !slices.Equal(r.Commands[0], []string{"remove", path}) {
		t.Fatal(r, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("dry run removed cache", err)
	}
	r, err = Apply(context.Background(), e, c, ApplyOpts{})
	if err != nil || len(r.Commands) != 1 || !slices.Equal(r.Commands[0], []string{"remove", path}) {
		t.Fatal(r, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("cache directory survived", err)
	}
	for _, call := range *calls {
		if strings.HasPrefix(call, "bun ") {
			t.Fatal("fixed cache cleanup ran bun", *calls)
		}
	}
}

func TestCacheMissingToolSkipped(t *testing.T) {
	e, _, _ := cacheFixture(t)
	cs, err := List(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		if c.ID == "cache:uv" {
			t.Fatal(c)
		}
	}
}

func TestCacheReportOnly(t *testing.T) {
	e, _, _ := cacheFixture(t)
	if err := os.MkdirAll(filepath.Join(e.Home, ".cache", "huggingface"), 0700); err != nil {
		t.Fatal(err)
	}
	c := cacheListed(t, e, "cache:huggingface")
	if c.Status != "report_only" {
		t.Fatal(c)
	}
	if _, err := Apply(context.Background(), e, c, ApplyOpts{}); err == nil || err.Error() != "report only" {
		t.Fatal(err)
	}
}

func TestCacheServiceRefused(t *testing.T) {
	e, calls, _ := cacheFixture(t)
	c := cacheListed(t, e, "cache:go-build")
	e.RunTools = false
	if _, err := Apply(context.Background(), e, c, ApplyOpts{}); err == nil || err.Error() != "cache cleanup runs from the driga CLI only" {
		t.Fatal(err)
	}
	if slices.Contains(*calls, "go clean -cache") {
		t.Fatal(*calls)
	}
	*calls = nil
	if _, err := List(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 0 {
		t.Fatal("service ran tools", *calls)
	}
}

func TestCacheRunsToolCommand(t *testing.T) {
	e, calls, _ := cacheFixture(t)
	c := cacheListed(t, e, "cache:go-build")
	r, err := Apply(context.Background(), e, c, ApplyOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(*calls, "go clean -cache") || len(r.Commands) != 1 || strings.Join(r.Commands[0], " ") != "go clean -cache" {
		t.Fatal(r, *calls)
	}
	b, err := os.ReadFile(filepath.Join(e.DataDir, "cleanup.log"))
	if err != nil || !strings.Contains(string(b), `"commands":[["go","clean","-cache"]]`) {
		t.Fatalf("%s %v", b, err)
	}
}

func TestCacheXcodeSymlinkDoesNotDeleteOtherDirectory(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Xcode cache is macOS-only")
	}
	e, _, _ := cacheFixture(t)
	parent := filepath.Join(e.Home, "Library/Developer/Xcode")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	target := filepath.Join(outside, "cache")
	put(t, filepath.Join(target, "keep"), "keep")
	other := filepath.Join(outside, "DerivedData", "keep")
	put(t, other, "keep")
	link := filepath.Join(parent, "DerivedData")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	c := cacheListed(t, e, "cache:xcode")
	if _, err := Apply(context.Background(), e, c, ApplyOpts{}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{other, filepath.Join(target, "keep")} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("outside file removed: %s: %v", p, err)
		}
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatal("cache link survived", err)
	}
}
