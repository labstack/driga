package cleanup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/store"
)

type cacheLivenessPlatform struct {
	platform.Fake
	procs     []model.Proc
	openPaths []string
	alive     bool
}

func (p *cacheLivenessPlatform) Procs(context.Context) ([]model.Proc, error) {
	return append([]model.Proc(nil), p.procs...), nil
}

func (p *cacheLivenessPlatform) Cwds(_ context.Context, pids []int32) (map[int32]string, error) {
	cwds := make(map[int32]string)
	for _, proc := range p.procs {
		if slices.Contains(pids, proc.PID) {
			cwds[proc.PID] = proc.Cwd
		}
	}
	return cwds, nil
}

func (p *cacheLivenessPlatform) Alive(int32, time.Time) bool { return p.alive }

func (p *cacheLivenessPlatform) OpenFilePaths(context.Context) ([]string, error) {
	return append([]string{}, p.openPaths...), nil
}

func cacheTx(t *testing.T, e Env, f func(*store.Tx) error) {
	t.Helper()
	if err := e.Store.Tx(context.Background(), f); err != nil {
		t.Fatal(err)
	}
}

func cacheAbsent(t *testing.T, e Env, path string) {
	t.Helper()
	cs, err := List(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		if c.Path == path {
			t.Fatalf("unsafe candidate: %+v", c)
		}
	}
}

type cacheOpenError struct{ *cacheLivenessPlatform }

func (p cacheOpenError) OpenFilePaths(context.Context) ([]string, error) {
	return nil, errors.New("inspection failed")
}

type cacheNoOpenLister struct{ platform.Platform }

func directCacheFixture(t *testing.T, kind string) (Env, *cacheLivenessPlatform, string) {
	t.Helper()
	e, _, _ := cacheFixture(t)
	p := &cacheLivenessPlatform{}
	e.Plat = p
	path := filepath.Join(e.Home, ".bun", "install", "cache")
	if kind == "xcode" {
		if runtime.GOOS != "darwin" {
			t.Skip("Xcode cache is macOS-only")
		}
		path = filepath.Join(e.Home, "Library", "Developer", "Xcode", "DerivedData")
	}
	put(t, filepath.Join(path, "nested", "keep"), "active artifact")
	return e, p, path
}

func TestDirectCacheRechecksLiveness(t *testing.T) {
	for _, kind := range []string{"bun", "xcode"} {
		for _, state := range []string{"cwd", "open file", "live session", "finished session", "open error", "no open lister", "unreadable cwd"} {
			for _, dry := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/%s/dry=%t", kind, state, dry), func(t *testing.T) {
					e, p, path := directCacheFixture(t, kind)
					c := cacheListed(t, e, "cache:"+kind)
					if c.Status != "ready" {
						t.Fatal(c)
					}
					if err := Verify(context.Background(), e, c); err != nil {
						t.Fatal(err)
					}
					want := state
					switch state {
					case "cwd":
						p.procs = []model.Proc{{PID: 123, Cwd: filepath.Join(path, "nested")}}
					case "open file":
						p.openPaths = []string{resolved(filepath.Join(path, "nested", "keep"))}
					case "live session", "finished session":
						label := "working"
						if state == "finished session" {
							label = "finished"
						}
						p.alive = true
						cacheTx(t, e, func(tx *store.Tx) error {
							_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "active", Cwd: filepath.Join(path, "nested"), PID: 123}, Label: label})
							return err
						})
						want = "live session"
					case "open error":
						e.Plat = cacheOpenError{p}
						want = "open-file inspection unavailable"
					case "no open lister":
						e.Plat = cacheNoOpenLister{p}
						want = "open-file inspection unavailable"
					case "unreadable cwd":
						p.alive = true
						p.procs = []model.Proc{{PID: 123}}
						want = "cannot inspect cwd"
					}
					r, err := Apply(context.Background(), e, c, ApplyOpts{DryRun: dry})
					if err == nil || !strings.Contains(err.Error(), want) {
						t.Errorf("active cache accepted: result=%+v err=%v, want %q", r, err, want)
					}
					if len(r.Commands) != 0 || r.FreedBytes != 0 {
						t.Errorf("refusal planned mutation: %+v", r)
					}
					if data, err := os.ReadFile(filepath.Join(path, "nested", "keep")); err != nil || string(data) != "active artifact" {
						t.Errorf("active content changed: %q %v", data, err)
					}
					if state == "unreadable cwd" {
						if _, err := List(context.Background(), e); err == nil {
							t.Fatal("unreadable process accepted at listing")
						}
						return
					}
					cacheAbsent(t, e, path)
					e.Why = true
					refused := cacheListed(t, e, c.ID)
					if refused.Status != "refused" || !strings.Contains(strings.Join(refused.Evidence, " "), want) {
						t.Fatalf("listing missed liveness: %+v", refused)
					}
				})
			}
		}
	}
}

func TestDirectCacheIdleRemoval(t *testing.T) {
	for _, kind := range []string{"bun", "xcode"} {
		t.Run(kind, func(t *testing.T) {
			e, p, path := directCacheFixture(t, kind)
			// Sibling prefixes and ended sessions do not own this cache.
			p.procs = []model.Proc{{PID: 123, Cwd: path + "-other"}}
			p.openPaths = []string{resolved(path) + "-other/file"}
			cacheTx(t, e, func(tx *store.Tx) error {
				_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "ended", Cwd: path}, Label: "ended"})
				return err
			})
			c := cacheListed(t, e, "cache:"+kind)
			if _, err := Apply(context.Background(), e, c, ApplyOpts{}); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("idle cache remains: %v", err)
			}
		})
	}
}

func TestCacheToolAndReportOnlyKeepTheirSemantics(t *testing.T) {
	e, calls, _ := cacheFixture(t)
	e.Plat = cacheOpenError{&cacheLivenessPlatform{}}
	c := cacheListed(t, e, "cache:go-build")
	if _, err := Apply(context.Background(), e, c, ApplyOpts{}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(*calls, "go clean -cache") {
		t.Fatal("native cleanup was not used")
	}
	path := filepath.Join(e.Home, ".cache", "huggingface")
	put(t, filepath.Join(path, "keep"), "model")
	c = cacheListed(t, e, "cache:huggingface")
	if c.Status != "report_only" {
		t.Fatal(c)
	}
	if _, err := Apply(context.Background(), e, c, ApplyOpts{}); err == nil || err.Error() != "report only" {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(path, "keep")); err != nil || string(data) != "model" {
		t.Fatal("report-only content changed", err)
	}
}
