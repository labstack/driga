package kill

import (
	"context"
	"errors"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
)

// Resource snapshots may omit this still-live process after a per-PID metadata
// failure. No native operation is available through this fixture.
type omittedProcessPlatform struct {
	platform.Platform
	proc  model.Proc
	calls int
	alive bool
}

func (p *omittedProcessPlatform) Procs(context.Context) ([]model.Proc, error) {
	p.calls++
	if p.calls == 1 {
		return []model.Proc{p.proc}, nil
	}
	return nil, nil
}

func (p *omittedProcessPlatform) Alive(int32, time.Time) bool { return p.alive }

type observedProcessPlatform struct {
	*omittedProcessPlatform
	observe func(context.Context, int32) (model.Proc, bool, error)
}

func (p observedProcessPlatform) ObserveProcess(ctx context.Context, pid int32) (model.Proc, bool, error) {
	return p.observe(ctx, pid)
}

func TestMissingResourceRowDoesNotProveExit(t *testing.T) {
	for _, alive := range []bool{false, true} {
		t.Run(map[bool]string{false: "liveness unavailable", true: "positive liveness"}[alive], func(t *testing.T) {
			proc := model.Proc{PID: 123456789, PPID: 1, StartTime: time.Now().Add(-time.Hour), RSS: 4096}
			p := &omittedProcessPlatform{proc: proc, alive: alive}
			r, err := tree(context.Background(), p, model.Orphan{RootPID: proc.PID, StartTime: proc.StartTime}, nil, false, time.Nanosecond, func(int, syscall.Signal) error { return nil })
			if err != nil || len(r.Stopped) != 0 || r.StoppedUsage.Processes != 0 || len(r.Survivors) != boolInt(alive) || len(r.Unknown) != boolInt(!alive) {
				t.Fatalf("missing metadata inferred exit: result=%+v err=%v", r, err)
			}
		})
	}
}

func TestMissingResourceRowUsesExplicitIdentityObservation(t *testing.T) {
	proc := model.Proc{PID: 123456789, PPID: 1, StartTime: time.Now().Add(-time.Hour), RSS: 4096}
	for _, name := range []string{"alive", "gone", "reused", "zombie", "unknown start", "permission", "deadline", "cancelled action"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wantStopped := name == "gone" || name == "reused" || name == "zombie" || name == "cancelled action"
			wantUnknown := name == "unknown start" || name == "permission" || name == "deadline"
			var observationErr error
			calls := 0
			p := observedProcessPlatform{omittedProcessPlatform: &omittedProcessPlatform{proc: proc, alive: true}, observe: func(finalCtx context.Context, pid int32) (model.Proc, bool, error) {
				calls++
				if pid != proc.PID || finalCtx.Err() != nil {
					t.Fatalf("wrong final identity/context: %d %v", pid, finalCtx.Err())
				}
				if _, bounded := finalCtx.Deadline(); !bounded {
					t.Fatal("final observation has no deadline")
				}
				current := proc
				switch name {
				case "gone", "cancelled action":
					return model.Proc{}, false, nil
				case "reused":
					current.StartTime = current.StartTime.Add(time.Second)
				case "zombie":
					current.Zombie = true
				case "unknown start":
					current.StartTime = time.Time{}
				case "permission":
					observationErr = os.ErrPermission
				case "deadline":
					observationErr = context.DeadlineExceeded
				}
				return current, true, observationErr
			}}
			r, err := tree(ctx, p, model.Orphan{RootPID: proc.PID, StartTime: proc.StartTime}, nil, false, time.Nanosecond, func(int, syscall.Signal) error {
				if name == "cancelled action" {
					cancel()
				}
				return nil
			})
			if calls != 1 || len(r.Stopped) != boolInt(wantStopped) || r.StoppedUsage.Processes != boolInt(wantStopped) || len(r.Unknown) != boolInt(wantUnknown) || len(r.Survivors) != boolInt(!wantStopped && !wantUnknown) {
				t.Fatalf("result=%+v err=%v calls=%d", r, err, calls)
			}
			if observationErr != nil && !errors.Is(err, observationErr) {
				t.Fatalf("observation failure lost: %v", err)
			}
		})
	}
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
