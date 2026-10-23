package viewer

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
)

type startupPlatform struct {
	calls   int
	started chan struct{}
	release chan struct{}
}

func (p *startupPlatform) Procs(ctx context.Context) ([]model.Proc, error) {
	p.calls++
	if p.calls == 2 {
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
