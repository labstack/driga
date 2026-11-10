package collector

import (
	"context"
	"sync"
	"time"

	"github.com/labstack/driga/internal/gitscan"
	"github.com/labstack/driga/internal/model"
)

// One inventory job owns the Git scanner at a time. Completed results stay here
// until their transaction commits; no queue can grow behind a slow repository.
type gitWorker struct {
	ctx     context.Context
	cancel  context.CancelFunc
	now     func() time.Time
	mu      sync.Mutex
	running bool
	ready   *gitJob
	wg      sync.WaitGroup
	wake    chan struct{}
}
type gitPaths struct{ cwds, roots, known []string }
type gitJob struct {
	results []repoResult
	err     error
	full    bool
	at      time.Time
}

func newGitWorker(ctx context.Context, now func() time.Time) *gitWorker {
	ctx, cancel := context.WithCancel(ctx)
	return &gitWorker{ctx: ctx, cancel: cancel, now: now, wake: make(chan struct{}, 1)}
}
func (w *gitWorker) close()        { w.cancel(); w.wg.Wait() }
func (w *gitWorker) isReady() bool { w.mu.Lock(); defer w.mu.Unlock(); return w.ready != nil }
func (w *gitWorker) ack(job *gitJob) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ready == job {
		w.ready = nil
	}
}
func (w *gitWorker) poll(d Deps, ss []model.Session, known []model.Repo, mode gitscan.ScanMode, now time.Time, request bool) *gitJob {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ready != nil {
		return w.ready
	}
	if !request || w.running || w.ctx.Err() != nil {
		return nil
	}
	// Retain only directory strings, rather than whole session transcripts or
	// process tables, while the slow job runs.
	input := gitInventory(d, ss, known, now)
	w.running = true
	w.wg.Go(func() {
		results, err := collectGitInventory(w.ctx, d, input, mode)
		job := &gitJob{results: results, err: err, full: mode == gitscan.Full, at: w.now()}
		w.mu.Lock()
		w.running, w.ready = false, job
		w.mu.Unlock()
		select {
		case w.wake <- struct{}{}:
		default:
		}
	})
	return nil
}
