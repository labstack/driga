package kill

import (
	"context"
	"syscall"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestWindowsTreeChangeGuard(t *testing.T) {
	start := time.Now()
	want := []model.Proc{{PID: 100, PPID: 50, StartTime: start}, {PID: 101, PPID: 100, StartTime: start}}
	for _, tc := range []struct {
		name    string
		ps      []model.Proc
		changed bool
	}{
		{"same", want, false},
		{"exited", want[:1], false},
		{"all exited", nil, false},
		{"new child", append(append([]model.Proc{}, want...), model.Proc{PID: 102, PPID: 101, StartTime: start}), true},
		{"reused child", []model.Proc{want[0], {PID: 101, PPID: 100, StartTime: start.Add(time.Second)}}, true},
		{"reused child within one clock tick", []model.Proc{want[0], {PID: 101, PPID: 100, StartTime: start.Add(100 * time.Nanosecond)}}, true},
		{"unknown child", []model.Proc{want[0], {PID: 101, PPID: 100}}, true},
		{"reparented", []model.Proc{want[0], {PID: 101, PPID: 60, StartTime: start}}, true},
		{"new child after root exits", []model.Proc{want[1], {PID: 102, PPID: 101, StartTime: start}}, true},
		{"new child after parent exits", []model.Proc{want[0], {PID: 102, PPID: 101, StartTime: start}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := windowsTreeChanged(want, tc.ps); (got != "") != tc.changed {
				t.Fatalf("change=%q", got)
			}
		})
	}
}

func TestWindowsTreeAbortsBeforeSignalAndDuringGrace(t *testing.T) {
	start := time.Now()
	ps := []model.Proc{{PID: 100, PPID: 50, StartTime: start}, {PID: 101, PPID: 100, StartTime: start}}
	changed := append(append([]model.Proc{}, ps...), model.Proc{PID: 102, PPID: 101, StartTime: start})
	for _, duringGrace := range []bool{false, true} {
		snaps := [][]model.Proc{ps, changed}
		if duringGrace {
			snaps = [][]model.Proc{ps, ps, ps, ps, changed}
		}
		p := seqProber{&seqPlatform{snaps: snaps}}
		o := model.Orphan{RootPID: 100, StartTime: start, ProcessStarts: map[int32]time.Time{100: start, 101: start}}
		var graceful, forced int
		r, err := runTree(context.Background(), p, o, []int32{101}, true, 5*time.Millisecond, func(_ model.Proc, sig syscall.Signal) error {
			if sig == syscall.SIGTERM {
				graceful++
			} else {
				forced++
			}
			return nil
		}, true)
		want := 0
		if duringGrace {
			want = 2
		}
		if err != nil || !r.Aborted || graceful != want || forced != 0 {
			t.Fatalf("grace=%t: %+v %v signals=%d/%d", duringGrace, r, err, graceful, forced)
		}
	}
}
