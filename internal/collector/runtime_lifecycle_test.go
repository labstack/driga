package collector

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/store"
)

func TestRuntimeStatsRejectPriorRunWhileSamePIDRestarts(t *testing.T) {
	d, _, _, adapter := setup(t)
	d.Cfg.Intake.OTLP = false
	d.Owner = Owner{Kind: "app", Instance: "caller-supplied-stale-instance"}
	dir := config.DataDir()
	ready := &intakeReadyClock{ready: make(chan struct{})}
	d.clock = ready
	start := func(d Deps) (context.CancelFunc, <-chan error) {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- Run(ctx, d, dir) }()
		t.Cleanup(cancel)
		return cancel, done
	}
	stop := func(cancel context.CancelFunc, done <-chan error) {
		t.Helper()
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("collector did not stop")
		}
	}
	cancel, done := start(d)
	select {
	case <-ready.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("first collector pass did not complete")
	}
	first, held, err := ReadOwner(dir)
	if err != nil || !held || first.PID != os.Getpid() || first.Instance == "" || first.Instance == d.Owner.Instance {
		t.Fatalf("first run owner: %+v, held=%t, error=%v", first, held, err)
	}
	reader, err := store.Open(filepath.Join(dir, "driga.db"), store.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	before, err := ReadRuntimeStats(reader, first, d.now(), time.Minute)
	if err != nil {
		t.Fatalf("first run sample unavailable: %v", err)
	}
	stop(cancel, done)

	// Stop the replacement's first adapter read before it can publish a new
	// sample. The previous sample remains young and has this exact OS PID.
	blocked := make(chan struct{})
	adapter.collect = func(ctx context.Context) { close(blocked); <-ctx.Done() }
	d.clock, d.State = nil, NewState()
	cancel, done = start(d)
	select {
	case <-blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("replacement collector did not reach its first pass")
	}
	second, held, err := ReadOwner(dir)
	if err != nil || !held || second.PID != first.PID || second.Instance == "" || second.Instance == first.Instance || second.Instance == d.Owner.Instance {
		t.Fatalf("replacement did not publish a fresh run identity: first=%+v second=%+v held=%t error=%v", first, second, held, err)
	}
	if old, err := ReadRuntimeStats(reader, first, d.now(), time.Minute); err != nil || old.Instance != before.Instance {
		t.Fatalf("startup fixture did not preserve the fresh prior-run sample: %+v, %v", old, err)
	}
	if _, err := ReadRuntimeStats(reader, second, d.now(), time.Minute); err == nil {
		t.Fatal("replacement collector borrowed previous run's same-PID sample")
	}
	stop(cancel, done)
}
