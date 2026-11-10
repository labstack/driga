package cleanup

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
)

const goCacheREADMEFixture = "This directory holds cached build artifacts from the Go build system.\nRun \"go clean -cache\" if the directory is getting too large.\n"

func goTmpCacheFixture(t *testing.T) (Env, string) {
	t.Helper()
	e, _, _ := cacheFixture(t)
	e.RunTools = false
	e.Now = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(resolved(e.TempRoots[0]), "arbitrary-task-cache")
	put(t, filepath.Join(path, "README"), goCacheREADMEFixture)
	put(t, filepath.Join(path, "ab", "artifact"), "cached")
	old := e.Now.Add(-48 * time.Hour)
	for _, p := range []string{filepath.Join(path, "README"), filepath.Join(path, "ab"), path} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	return e, path
}

func goTmpCacheListed(t *testing.T, e Env, path string) Candidate {
	t.Helper()
	cs, err := List(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		if c.Path == path && strings.HasPrefix(c.ID, "cache:go-tmp:") {
			return c
		}
	}
	t.Fatalf("missing temp Go cache %s: %+v", path, cs)
	return Candidate{}
}

func TestGoTmpCacheListed(t *testing.T) {
	e, path := goTmpCacheFixture(t)
	e.TempRoots = append(e.TempRoots, e.TempRoots[0])
	c := goTmpCacheListed(t, e, path)
	wantID := fmt.Sprintf("cache:go-tmp:%x", sha256.Sum256([]byte(path)))[:25]
	wantEvidence := []string{"Go build cache in temp, idle since " + e.Now.Add(-48*time.Hour).Format(time.RFC3339), "clean with: remove " + path}
	if c.ID != wantID || c.Kind != Cache || c.Status != "ready" || !slices.Equal(c.Evidence, wantEvidence) {
		t.Fatal(c)
	}
	cs, err := List(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, candidate := range cs {
		if candidate.ID == c.ID {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("duplicate cache: %d", count)
	}
}

func TestGoTmpCacheRecentChildRefused(t *testing.T) {
	e, path := goTmpCacheFixture(t)
	child := filepath.Join(path, "ab")
	if err := os.Chtimes(child, e.Now, e.Now); err != nil {
		t.Fatal(err)
	}
	cs, err := List(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		if c.Path == path {
			t.Fatal("recent cache listed without --why", c)
		}
	}
	e.Why = true
	c := goTmpCacheListed(t, e, path)
	if c.Status != "refused" || !strings.Contains(strings.Join(c.Evidence, " "), "24h") {
		t.Fatal(c)
	}
}

type goTmpCachePlatform struct {
	platform.Fake
	cwd, open string
	openErr   error
}

func (p *goTmpCachePlatform) Procs(context.Context) ([]model.Proc, error) {
	return []model.Proc{{PID: 123, Cwd: p.cwd}}, nil
}

func (p *goTmpCachePlatform) Cwds(context.Context, []int32) (map[int32]string, error) {
	return map[int32]string{123: p.cwd}, nil
}

func (p *goTmpCachePlatform) OpenFilePaths(context.Context) ([]string, error) {
	return []string{p.open}, p.openErr
}

func TestGoTmpCacheCwdRefused(t *testing.T) {
	e, path := goTmpCacheFixture(t)
	e.Plat = &goTmpCachePlatform{cwd: filepath.Join(path, "ab")}
	e.Why = true
	c := goTmpCacheListed(t, e, path)
	if c.Status != "refused" || !strings.Contains(strings.Join(c.Evidence, " "), "process cwd") {
		t.Fatal(c)
	}
}

func TestGoTmpCacheOpenFileRefused(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		t.Run(fmt.Sprint(unavailable), func(t *testing.T) {
			e, path := goTmpCacheFixture(t)
			p := &goTmpCachePlatform{open: filepath.Join(path, "ab", "artifact")}
			want := "open file"
			if unavailable {
				p.openErr = errors.New("unavailable")
				want = "inspection unavailable"
			}
			e.Plat, e.Why = p, true
			c := goTmpCacheListed(t, e, path)
			if c.Status != "refused" || !strings.Contains(strings.Join(c.Evidence, " "), want) {
				t.Fatal(c)
			}
		})
	}
}

func goTmpCacheAbsent(t *testing.T, e Env, path string) {
	t.Helper()
	cs, err := List(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		if c.Path == path && strings.HasPrefix(c.ID, "cache:go-tmp:") {
			t.Fatal("unexpected temp Go cache", c)
		}
	}
}

func TestGoTmpCacheConfiguredCacheSkipped(t *testing.T) {
	e, path := goTmpCacheFixture(t)
	t.Setenv("GOCACHE", "")
	e.RunTools = true
	e.Why = true
	alias := filepath.Join(t.TempDir(), "active-cache")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	e.Tool = func(_ context.Context, binary string, args ...string) ([]byte, error) {
		if binary != "go" || !slices.Equal(args, []string{"env", "GOCACHE"}) {
			return nil, errors.New("tool unavailable")
		}
		return []byte(alias + "\n"), nil
	}
	goTmpCacheAbsent(t, e, path)
}

func TestGoTmpCacheConfiguredToolRechecked(t *testing.T) {
	e, path := goTmpCacheFixture(t)
	e.RunTools = true
	active := e.Home
	e.Tool = func(_ context.Context, binary string, args ...string) ([]byte, error) {
		if binary == "go" && slices.Equal(args, []string{"env", "GOCACHE"}) {
			return []byte(active + "\n"), nil
		}
		return nil, errors.New("tool unavailable")
	}
	c := goTmpCacheListed(t, e, path)
	active = path
	if _, err := Apply(context.Background(), e, c, ApplyOpts{}); err == nil || !strings.Contains(err.Error(), "configured GOCACHE") {
		t.Fatalf("configured cache accepted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(path, "ab", "artifact")); err != nil {
		t.Fatal("configured cache was removed", err)
	}
}

func TestGoTmpCacheToolsDisabled(t *testing.T) {
	e, path := goTmpCacheFixture(t)
	t.Setenv("GOCACHE", "")
	e.Tool = func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("tools executed with RunTools=false")
		return nil, errors.New("tools disabled")
	}
	c := goTmpCacheListed(t, e, path)
	for _, dryRun := range []bool{true, false} {
		if _, err := Apply(context.Background(), e, c, ApplyOpts{DryRun: dryRun}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("cache survived removal", err)
	}
}

func TestCacheToolRequiresRunTools(t *testing.T) {
	calls := 0
	e := Env{Tool: func(context.Context, string, ...string) ([]byte, error) {
		calls++
		return nil, nil
	}}
	if _, err := e.tool(context.Background(), "go", "env", "GOCACHE"); err == nil || calls != 0 {
		t.Fatalf("disabled runner: calls=%d err=%v", calls, err)
	}
	e.RunTools = true
	if _, err := e.tool(context.Background(), "go", "env", "GOCACHE"); err != nil || calls != 1 {
		t.Fatalf("enabled runner: calls=%d err=%v", calls, err)
	}
}

func TestGoTmpCacheExportedCacheSkippedWithoutTools(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		t.Run(fmt.Sprint(symlink), func(t *testing.T) {
			e, path := goTmpCacheFixture(t)
			active := path
			if symlink {
				active = filepath.Join(t.TempDir(), "active-cache")
				if err := os.Symlink(path, active); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("GOCACHE", active)
			e.Tool = func(context.Context, string, ...string) ([]byte, error) {
				t.Fatal("tools executed with RunTools=false")
				return nil, errors.New("tools disabled")
			}
			goTmpCacheAbsent(t, e, path)
		})
	}
}

func TestGoTmpCacheFingerprintRequired(t *testing.T) {
	for _, variant := range []string{"different README", "symlink directory", "symlink README", "nested cache"} {
		t.Run(variant, func(t *testing.T) {
			e, path := goTmpCacheFixture(t)
			e.Why = true
			switch variant {
			case "different README":
				put(t, filepath.Join(path, "README"), " This directory holds cached build artifacts from the Go build system.\n")
			case "symlink directory":
				target := filepath.Join(t.TempDir(), "cache")
				if err := os.Rename(path, target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			case "symlink README":
				readme := filepath.Join(path, "README")
				target := filepath.Join(t.TempDir(), "README")
				if err := os.Rename(readme, target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, readme); err != nil {
					t.Fatal(err)
				}
			case "nested cache":
				e.TempRoots = []string{filepath.Dir(filepath.Dir(path))}
			}
			goTmpCacheAbsent(t, e, path)
		})
	}
}

func TestGoTmpCacheLocateFailureSkipsNothing(t *testing.T) {
	e, path := goTmpCacheFixture(t)
	e.RunTools = true
	e.Tool = func(context.Context, string, ...string) ([]byte, error) {
		return []byte(path), errors.New("go unavailable")
	}
	if c := goTmpCacheListed(t, e, path); c.Status != "ready" {
		t.Fatal(c)
	}
}

func TestGoTmpCacheApply(t *testing.T) {
	e, path := goTmpCacheFixture(t)
	c := goTmpCacheListed(t, e, path)
	r, err := Apply(context.Background(), e, c, ApplyOpts{DryRun: true})
	if err != nil || len(r.Commands) != 1 || !slices.Equal(r.Commands[0], []string{"remove", path}) {
		t.Fatal(r, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("dry run removed cache", err)
	}
	r, err = Apply(context.Background(), e, c, ApplyOpts{})
	if err != nil || len(r.Commands) != 1 || !slices.Equal(r.Commands[0], []string{"remove", path}) || r.FreedBytes < 0 {
		t.Fatal(r, err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("cache survived removal", err)
	}
	log, err := os.ReadFile(filepath.Join(e.DataDir, "cleanup.log"))
	if err != nil || !strings.Contains(string(log), `"commands":[["remove",`) {
		t.Fatalf("missing removal audit: %s: %v", log, err)
	}
}

func TestGoTmpCacheApplyRechecks(t *testing.T) {
	for _, change := range []string{"symlink", "replacement", "README", "recent child", "cwd", "open file", "configured cache"} {
		t.Run(change, func(t *testing.T) {
			e, path := goTmpCacheFixture(t)
			c := goTmpCacheListed(t, e, path)
			switch change {
			case "symlink", "replacement":
				original := filepath.Join(t.TempDir(), "original")
				if err := os.Rename(path, original); err != nil {
					t.Fatal(err)
				}
				if change == "symlink" {
					if err := os.Symlink(original, path); err != nil {
						t.Fatal(err)
					}
				} else {
					put(t, filepath.Join(path, "README"), goCacheREADMEFixture)
					old := e.Now.Add(-48 * time.Hour)
					for _, p := range []string{filepath.Join(path, "README"), path} {
						if err := os.Chtimes(p, old, old); err != nil {
							t.Fatal(err)
						}
					}
				}
			case "README":
				put(t, filepath.Join(path, "README"), "not a Go cache")
			case "recent child":
				if err := os.Chtimes(filepath.Join(path, "ab"), e.Now, e.Now); err != nil {
					t.Fatal(err)
				}
			case "cwd":
				e.Plat = &goTmpCachePlatform{cwd: filepath.Join(path, "ab")}
			case "open file":
				e.Plat = &goTmpCachePlatform{open: filepath.Join(path, "ab", "artifact")}
			case "configured cache":
				t.Setenv("GOCACHE", path)
			}
			if _, err := Apply(context.Background(), e, c, ApplyOpts{}); err == nil {
				t.Fatal("changed cache was removed")
			}
			if _, err := os.Lstat(path); err != nil {
				t.Fatal("refused cache disappeared", err)
			}
			if change == "symlink" {
				if _, err := os.Stat(filepath.Join(path, "ab", "artifact")); err != nil {
					t.Fatal("symlink target was modified", err)
				}
			}
		})
	}
}

func TestGoTmpCacheCanonicalRootDeduplication(t *testing.T) {
	e, path := goTmpCacheFixture(t)
	alias := filepath.Join(t.TempDir(), "temp-alias")
	if err := os.Symlink(e.TempRoots[0], alias); err != nil {
		t.Fatal(err)
	}
	e.TempRoots = append(e.TempRoots, alias)
	cs, err := List(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, c := range cs {
		if strings.HasPrefix(c.ID, "cache:go-tmp:") {
			count++
			if c.Path != path {
				t.Fatal("noncanonical cache path", c)
			}
		}
	}
	if count != 1 {
		t.Fatalf("want one cache across root aliases, got %d", count)
	}
}
