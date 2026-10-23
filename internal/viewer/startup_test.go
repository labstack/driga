package viewer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/labstack/driga/internal/adapter"
	"github.com/labstack/driga/internal/adapter/claude"
	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/gitscan"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/signatures"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

type startupPlatform struct {
	calls   int
	started chan struct{}
	release chan struct{}
}

func (p *startupPlatform) Procs(ctx context.Context) ([]model.Proc, error) {
	p.calls++
	if p.calls == 2 && p.started != nil {
		close(p.started)
		select {
		case <-p.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, nil
}
func (*startupPlatform) Machine(context.Context) (model.MachineSample, error) {
	return model.MachineSample{Cores: 2, MemTotal: 1024}, nil
}
func (*startupPlatform) Cwds(context.Context, []int32) (map[int32]string, error) {
	return nil, nil
}
func (*startupPlatform) Alive(int32, time.Time) bool { return false }

func TestLiveSnapshotPublishesBeforeGitAndDoesNotBlockReads(t *testing.T) {
	root := t.TempDir()
	// A Git scan would report an error for this deliberately invalid repo.
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Roots = []string{root}
	p := &startupPlatform{started: make(chan struct{}), release: make(chan struct{})}
	var once sync.Once
	unblock := func() { once.Do(func() { close(p.release) }) }
	defer unblock()
	s, err := NewLiveSnapshotSource(collector.Deps{Cfg: cfg, Plat: p, SpoolDir: filepath.Join(root, "spool")}, root, view.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s.Start(ctx)
	defer s.Close()
	snap, err := s.Load(ctx)
	if err != nil || snap.Machine.Sample.Cores != 2 {
		t.Fatalf("first machine snapshot: %+v %v", snap.Machine.Sample, err)
	}
	if _, scanned := snap.Freshness["git"]; scanned {
		t.Fatal("Git blocked the initial interactive snapshot")
	}
	select {
	case <-p.started:
	case <-ctx.Done():
		t.Fatal("second collection never started")
	}
	// Collection is still blocked, but the previous committed data is usable.
	readCtx, stopRead := context.WithTimeout(ctx, time.Second)
	defer stopRead()
	readDone := make(chan error, 1)
	go func() { _, err := s.Load(readCtx); readDone <- err }()
	select {
	case err := <-readDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-readCtx.Done():
		unblock()
		t.Fatal("background collection blocked a viewer read")
	}
	unblock()
	// Deferred enrichment must still run; a failed Git scan is source health,
	// not a reason to withhold the existing session/machine snapshot.
	for {
		snap, err := s.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, scanned := snap.Freshness["git"]; scanned {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("deferred Git enrichment never ran")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

type startupScanner struct {
	scanner          *gitscan.Scanner
	started, release chan struct{}
}

func (s *startupScanner) Scan(ctx context.Context, path string, mode gitscan.ScanMode) ([]model.Worktree, error) {
	if mode != gitscan.Identity {
		close(s.started)
		select {
		case <-s.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return s.scanner.Scan(ctx, path, mode)
}

func TestLiveSnapshotFirstFrameResolvesProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	t.Setenv("DRIGA_DATA_DIR", filepath.Join(home, "data"))
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo := filepath.Join(home, "repo")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", "-b", "main", repo)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	repo = gitscan.NormalizePath(repo)
	registry := filepath.Join(home, ".claude", "sessions")
	if err := os.MkdirAll(registry, 0700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{"sessionId": "startup", "cwd": repo, "kind": "interactive", "version": "2.1.7", "startedAt": time.Now().UnixMilli(), "status": "busy"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(registry, "4242.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Roots = nil
	t.Chdir(repo)
	p := &startupPlatform{}
	scanner := &startupScanner{scanner: gitscan.NewScanner(1), started: make(chan struct{}), release: make(chan struct{})}
	s, err := NewLiveSnapshotSource(collector.Deps{Cfg: cfg, Plat: p, Git: scanner, Adapters: []adapter.Adapter{claude.New()}, SpoolDir: filepath.Join(home, "spool")}, home, view.Filter{Project: ".", All: true})
	if err != nil {
		t.Fatal(err)
	}
	s.ResolveProject = func(r view.Reader, project string) (string, error) {
		project = gitscan.NormalizePath(project)
		repos, err := r.Repos()
		if err != nil {
			return "", err
		}
		for _, row := range repos {
			if row.Path == project {
				return row.Path, nil
			}
		}
		return "", fmt.Errorf("unknown project %q", project)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	s.Start(ctx)
	defer func() { cancel(); s.Close() }()
	snap, err := s.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Projects.Rows) != 1 || len(snap.Agents.Rows) != 1 {
		t.Fatalf("first frame: projects=%d agents=%d", len(snap.Projects.Rows), len(snap.Agents.Rows))
	}

	worktrees, err := s.st.Worktrees(0)
	if err != nil || len(worktrees) != 1 || worktrees[0].Branch != "main" {
		t.Fatalf("first worktree identity: %+v %v", worktrees, err)
	}
	if _, scanned := snap.Freshness["git"]; scanned {
		t.Fatal("first frame ran Git enrichment")
	}
	if _, err := s.LoadScope(ctx, store.Scope{}, true); err != nil {
		t.Fatal(err)
	}
	select {
	case <-scanner.started:
	case <-ctx.Done():
		t.Fatal("enrichment never started")
	}
	readCtx, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	if _, err := s.Load(readCtx); err != nil {
		t.Fatalf("project read during enrichment: %v", err)
	}
}

type startupSocketPlatform struct{ startupPlatform }

func (*startupSocketPlatform) Procs(context.Context) ([]model.Proc, error) {
	return []model.Proc{{PID: 200, PPID: 1, StartTime: time.Now().Add(-time.Hour), Cmdline: "claude mcp serve", Argv: []string{"claude", "mcp", "serve"}}}, nil
}
func (p *startupSocketPlatform) ExternalPeers(ctx context.Context, _ []int32) (map[int32]bool, error) {
	close(p.started)
	select {
	case <-p.release:
		return map[int32]bool{200: false}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func TestLiveSnapshotSocketProbeDoesNotBlockReads(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Roots = nil
	p := &startupSocketPlatform{startupPlatform{started: make(chan struct{}), release: make(chan struct{})}}
	s, err := NewLiveSnapshotSource(collector.Deps{Cfg: cfg, Plat: p, Sigs: signatures.Builtin(), SpoolDir: filepath.Join(root, "spool")}, root, view.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	s.Start(ctx)
	defer func() { cancel(); s.Close() }()
	if _, err := s.Load(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.started:
	case <-ctx.Done():
		t.Fatal("socket probe never started")
	}
	done := make(chan error, 1)
	go func() {
		readCtx, stop := context.WithTimeout(ctx, time.Second)
		defer stop()
		if _, err := s.Load(readCtx); err != nil {
			done <- err
			return
		}
		if _, err := s.DataVersion(readCtx); err != nil {
			done <- err
			return
		}
		s.SetActive(false)
		done <- nil
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("blocked socket probe stalled viewer: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked socket probe stalled viewer")
	}
}
