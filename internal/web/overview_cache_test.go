package web

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/panel"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/viewer"
)

type countingSource struct {
	Source
	loads int
}

func (s *countingSource) LoadScope(ctx context.Context, scope store.Scope, all ...bool) (viewer.Snapshot, error) {
	s.loads++
	return s.Source.LoadScope(ctx, scope, all...)
}
func TestOverviewAndCleanupCaches(t *testing.T) {
	base, _, _, _ := fixture(t)
	source := &countingSource{Source: base.source}
	s, err := New(source, base.opts)
	if err != nil {
		t.Fatal(err)
	}
	cleanups := 0
	s.opts.CleanupList = func(context.Context) ([]cleanup.Candidate, error) { cleanups++; return nil, nil }
	for i := 0; i < 2; i++ {
		if _, err := s.runPanels(context.Background(), panel.RunRequest{Dashboard: panel.Dashboard{Name: "x", Panels: []panel.Panel{{ID: "c", Title: "C", Viz: "stat", Source: "cleanup"}}}}); err != nil {
			t.Fatal(err)
		}
	}
	if source.loads != 1 || cleanups != 1 {
		t.Fatalf("loads=%d cleanups=%d", source.loads, cleanups)
	}
}

type cacheSource struct {
	load    func(context.Context, store.Scope) (viewer.Snapshot, error)
	version func(context.Context) (int64, error)
}

func (s cacheSource) DataVersion(ctx context.Context) (int64, error) {
	if s.version != nil {
		return s.version(ctx)
	}
	return 1, nil
}
func (s cacheSource) LoadScope(ctx context.Context, scope store.Scope, _ ...bool) (viewer.Snapshot, error) {
	return s.load(ctx, scope)
}
func cacheResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(time.Second):
		t.Fatal("cache request blocked behind unrelated I/O")
		return nil
	}
}
func TestSnapshotCacheIndependentKeys(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	s := &Server{source: cacheSource{load: func(_ context.Context, scope store.Scope) (viewer.Snapshot, error) {
		if scope.Machines[0] == "slow" {
			close(started)
			<-release
		}
		return viewer.Snapshot{}, nil
	}}}
	result := make(chan error, 1)
	go func() {
		_, err := s.cachedLoad(context.Background(), store.Scope{Machines: []string{"slow"}}, false)
		result <- err
	}()
	<-started
	defer close(release)
	other := make(chan error, 1)
	go func() {
		_, err := s.cachedLoad(context.Background(), store.Scope{Machines: []string{"fast"}}, false)
		other <- err
	}()
	if err := cacheResult(t, other); err != nil {
		t.Fatal(err)
	}
}
func TestCleanupDoesNotBlockSnapshots(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	s := &Server{source: cacheSource{load: func(context.Context, store.Scope) (viewer.Snapshot, error) { return viewer.Snapshot{}, nil }}}
	s.opts.CleanupList = func(context.Context) ([]cleanup.Candidate, error) { close(started); <-release; return nil, nil }
	result := make(chan error, 1)
	go func() { _, err := s.cachedCleanup(context.Background()); result <- err }()
	<-started
	defer close(release)
	other := make(chan error, 1)
	go func() { _, err := s.cachedLoad(context.Background(), store.Scope{}, false); other <- err }()
	if err := cacheResult(t, other); err != nil {
		t.Fatal(err)
	}
}
func TestSnapshotCacheCoalesces(t *testing.T) {
	const callers = 12
	versions := make(chan struct{}, callers)
	release := make(chan struct{})
	var loads atomic.Int32
	s := &Server{source: cacheSource{
		version: func(context.Context) (int64, error) { versions <- struct{}{}; return 1, nil },
		load: func(context.Context, store.Scope) (viewer.Snapshot, error) {
			loads.Add(1)
			<-release
			return viewer.Snapshot{}, nil
		},
	}}
	result := make(chan error, callers)
	for range callers {
		go func() { _, err := s.cachedLoad(context.Background(), store.Scope{}, false); result <- err }()
	}
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	for range callers {
		select {
		case <-versions:
		case <-time.After(time.Second):
			t.Fatal("DataVersion blocked behind a source read")
		}
	}
	close(release)
	released = true
	for range callers {
		if err := cacheResult(t, result); err != nil {
			t.Fatal(err)
		}
	}
	if loads.Load() != 1 {
		t.Fatalf("same key loaded %d times", loads.Load())
	}
}
func TestCacheWaitersCanCancel(t *testing.T) {
	for _, kind := range []string{"snapshot", "cleanup"} {
		t.Run(kind, func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			s := &Server{source: cacheSource{load: func(context.Context, store.Scope) (viewer.Snapshot, error) {
				close(started)
				<-release
				return viewer.Snapshot{}, nil
			}}}
			s.opts.CleanupList = func(context.Context) ([]cleanup.Candidate, error) { close(started); <-release; return nil, nil }
			load := func(ctx context.Context) error {
				if kind == "snapshot" {
					_, err := s.cachedLoad(ctx, store.Scope{}, false)
					return err
				}
				_, err := s.cachedCleanup(ctx)
				return err
			}
			leader := make(chan error, 1)
			go func() { leader <- load(context.Background()) }()
			<-started
			defer close(release)
			ctx, cancel := context.WithCancel(context.Background())
			waiter := make(chan error, 1)
			go func() { waiter <- load(ctx) }()
			cancel()
			if err := cacheResult(t, waiter); !errors.Is(err, context.Canceled) {
				t.Fatalf("waiter: %v", err)
			}
		})
	}
}
func TestCleanupCachesOnlySuccess(t *testing.T) {
	calls := 0
	s := &Server{}
	s.opts.CleanupList = func(context.Context) ([]cleanup.Candidate, error) {
		calls++
		switch calls {
		case 1:
			return nil, errors.New("disk read failed")
		case 2:
			return nil, context.Canceled
		}
		return []cleanup.Candidate{{}}, nil
	}
	for i := 0; i < 2; i++ {
		if _, err := s.cachedCleanup(context.Background()); err == nil {
			t.Fatal("expected failure")
		}
	}
	for range 2 {
		if rows, err := s.cachedCleanup(context.Background()); err != nil || len(rows) != 1 {
			t.Fatalf("retry: %v %v", rows, err)
		}
	}
	if calls != 3 {
		t.Fatalf("cleanup called %d times, want failed reads retried then success cached", calls)
	}
}
func TestCancelledSnapshotLeaderDoesNotPoisonWaiter(t *testing.T) {
	started, waiting := make(chan struct{}), make(chan struct{}, 3)
	var calls atomic.Int32
	s := &Server{source: cacheSource{
		version: func(context.Context) (int64, error) { waiting <- struct{}{}; return 1, nil },
		load: func(ctx context.Context, _ store.Scope) (viewer.Snapshot, error) {
			if calls.Add(1) == 1 {
				close(started)
				<-ctx.Done()
				return viewer.Snapshot{}, ctx.Err()
			}
			return viewer.Snapshot{}, nil
		},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	leader, waiter := make(chan error, 1), make(chan error, 1)
	go func() { _, err := s.cachedLoad(ctx, store.Scope{}, false); leader <- err }()
	<-started
	<-waiting
	go func() { _, err := s.cachedLoad(context.Background(), store.Scope{}, false); waiter <- err }()
	select {
	case <-waiting:
	case <-time.After(time.Second):
		t.Fatal("waiter could not inspect cache")
	}
	cancel()
	if err := cacheResult(t, leader); !errors.Is(err, context.Canceled) {
		t.Fatalf("leader: %v", err)
	}
	if err := cacheResult(t, waiter); err != nil {
		t.Fatalf("waiter inherited leader cancellation: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("loads %d", calls.Load())
	}
}

func TestCleanupSizesReachHeaderWithoutRelisting(t *testing.T) {
	s, _, _, _ := fixture(t)
	var candidates []cleanup.Candidate
	for i, contents := range []string{"abc", "12345"} {
		path := filepath.Join(t.TempDir(), "data")
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		candidates = append(candidates, cleanup.Candidate{ID: string(rune('a' + i)), Path: path, Status: "ready"})
	}
	candidates = append(candidates, cleanup.Candidate{Status: "refused", Size: 1000}, cleanup.Candidate{Status: "report_only", Size: 1000})
	calls := 0
	s.opts.CleanupList = func(context.Context) ([]cleanup.Candidate, error) { calls++; return candidates, nil }
	read := func() any {
		t.Helper()
		f, err := s.runPanels(context.Background(), panel.RunRequest{Dashboard: panel.Header()})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range f.Results {
			if r.ID == "reclaim" {
				return r.Frame.Rows[0][0]
			}
		}
		t.Fatal("missing reclaim")
		return nil
	}
	if v := read(); v != nil {
		t.Fatalf("first load = %v, want unknown", v)
	}
	deadline := time.Now().Add(3 * time.Second)
	for read() != float64(8) {
		if time.Now().After(deadline) {
			t.Fatal("header never received measured sum")
		}
		time.Sleep(time.Millisecond)
	}
	if calls != 1 {
		t.Fatalf("relisted %d times", calls)
	}
	candidates = candidates[1:]
	s.cleanupAt = time.Time{}
	if v := read(); v != float64(5) {
		t.Fatalf("removed candidate still counted: %v", v)
	}
}
