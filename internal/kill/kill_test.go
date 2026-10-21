//go:build !windows

package kill

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
)

type socketPlatform struct {
	snapshotPlatform
	peers  map[int32]bool
	err    error
	probed []int32
}

func (p *socketPlatform) ExternalPeers(_ context.Context, pids []int32) (map[int32]bool, error) {
	p.probed = append([]int32(nil), pids...)
	return p.peers, p.err
}

func TestKillAbortsOnExternalPeer(t *testing.T) {
	now := time.Now()
	for _, active := range []int32{100, 200} {
		t.Run(fmt.Sprint(active), func(t *testing.T) {
			aliveCalls := 0
			p := &socketPlatform{snapshotPlatform: snapshotPlatform{
				procs: []model.Proc{{PID: 100, StartTime: now}, {PID: 200, PPID: 100, StartTime: now}},
				// Even the RED run cannot send a real signal: all synthetic PIDs
				// report dead if the socket check is incorrectly skipped.
				alive: func(int32, time.Time) bool { aliveCalls++; return false },
			}, peers: map[int32]bool{100: active == 100, 200: active == 200}}
			r, err := Tree(context.Background(), p, model.Orphan{RootPID: 100, StartTime: now, Verified: true}, []int32{200}, true, time.Millisecond)
			slices.Sort(p.probed)
			if err != nil || !r.Aborted || !strings.Contains(r.Reason, "external peer") || len(r.Signaled) != 0 || aliveCalls != 0 || !slices.Equal(p.probed, []int32{100, 200}) {
				t.Fatalf("%+v %v probed=%v alive=%d", r, err, p.probed, aliveCalls)
			}
		})
	}
}

func TestKillAbortsOnSocketProbeError(t *testing.T) {
	now := time.Now()
	p := &socketPlatform{snapshotPlatform: snapshotPlatform{
		procs: []model.Proc{{PID: 100, StartTime: now}},
		alive: func(int32, time.Time) bool { return false },
	}, err: errors.New("socket check failed")}
	r, err := Tree(context.Background(), p, model.Orphan{RootPID: 100, StartTime: now, Verified: true}, nil, false, time.Millisecond)
	if !r.Aborted || err == nil || len(r.Signaled) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestKillAbortsOnChangedStartWithinRegistryWindow(t *testing.T) {
	now := time.Now()
	p := snapshotPlatform{procs: []model.Proc{{PID: 100, StartTime: now}}, alive: func(int32, time.Time) bool { return false }}
	r, err := Tree(context.Background(), p, model.Orphan{RootPID: 100, StartTime: now.Add(30 * time.Second), Verified: true}, nil, false, time.Millisecond)
	if err != nil || !r.Aborted || !strings.Contains(r.Reason, "start time changed") {
		t.Fatalf("%+v %v", r, err)
	}
}

// Each PID eligible for cleanup is reported by the shell that spawned it.
func childTree(t *testing.T) (platform.Platform, model.Orphan, []int32, func() int32) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("requires the real Darwin platform")
	}
	cmd := exec.Command("sh", "-c", `sleep 60 & a=$!; sleep 60 & b=$!; printf '%s %s\n' "$a" "$b"; read extra; if [ "$extra" = extra ]; then sleep 60 & printf '%s\n' "$!"; fi; wait`)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(waited) }()
	var children []int32
	t.Cleanup(func() {
		for _, pid := range children {
			_ = syscall.Kill(int(pid), syscall.SIGKILL)
		}
		_ = cmd.Process.Kill()
		<-waited
		_ = in.Close()
	})
	scan := bufio.NewScanner(out)
	readPIDs := func() []int32 {
		t.Helper()
		if !scan.Scan() {
			t.Fatalf("child PIDs: %v", scan.Err())
		}
		var pids []int32
		for _, field := range strings.Fields(scan.Text()) {
			pid, err := strconv.ParseInt(field, 10, 32)
			if err != nil || pid <= 1 {
				t.Fatalf("invalid child PID %q", field)
			}
			pids = append(pids, int32(pid))
			children = append(children, int32(pid))
		}
		return pids
	}
	pids := readPIDs()
	if len(pids) != 2 {
		t.Fatalf("children: %v", pids)
	}
	plat := platform.New()
	procs, err := plat.Procs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	idx := derive.NewProcIndex(procs)
	root, ok := idx.Get(int32(cmd.Process.Pid))
	if !ok {
		t.Fatal("root missing")
	}
	desc := idx.Descendants(root.PID)
	if len(desc) != 2 {
		t.Fatalf("descendants: %+v", desc)
	}
	o := model.Orphan{ID: fmt.Sprintf("%d:%d", root.PID, root.StartTime.Unix()), RootPID: root.PID, StartTime: root.StartTime, PIDs: append([]int32{root.PID}, pids...)}
	extra := func() int32 {
		t.Helper()
		if _, err := fmt.Fprintln(in, "extra"); err != nil {
			t.Fatal(err)
		}
		return readPIDs()[0]
	}
	return plat, o, pids, extra
}

func TestKillTerminatesTree(t *testing.T) {
	plat, o, pids, _ := childTree(t)
	slices.Reverse(pids) // Membership is a set, independent of display order.
	r, err := Tree(context.Background(), plat, o, pids, false, 2*time.Second)
	if err != nil || r.Aborted || len(r.Signaled) != 3 || len(r.Survivors) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	if r.Signaled[len(r.Signaled)-1] != o.RootPID {
		t.Fatalf("root must be last: %+v", r)
	}
	for _, pid := range o.PIDs {
		if plat.Alive(pid, o.StartTime) {
			t.Fatalf("PID %d still alive", pid)
		}
	}
}

func TestKillAbortsOnChangedTree(t *testing.T) {
	plat, o, pids, extra := childTree(t)
	pid := extra()
	r, err := Tree(context.Background(), plat, o, pids, true, time.Millisecond)
	if err != nil || !r.Aborted || r.Reason == "" || len(r.Signaled) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	for _, p := range append(o.PIDs, pid) {
		if !plat.Alive(p, o.StartTime) {
			t.Fatalf("PID %d was signaled", p)
		}
	}
}

func TestKillAbortsOnPidReuse(t *testing.T) {
	plat, o, pids, _ := childTree(t)
	o.StartTime = o.StartTime.Add(-time.Hour)
	r, err := Tree(context.Background(), plat, o, pids, false, time.Millisecond)
	if err != nil || !r.Aborted || r.Reason == "" || len(r.Signaled) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	if !plat.Alive(o.RootPID, o.StartTime.Add(time.Hour)) {
		t.Fatal("root was signaled")
	}
}

type snapshotPlatform struct {
	platform.Platform
	procs []model.Proc
	alive func(int32, time.Time) bool
}

func (p snapshotPlatform) Procs(context.Context) ([]model.Proc, error) { return p.procs, nil }

func (p snapshotPlatform) Alive(pid int32, start time.Time) bool { return p.alive(pid, start) }

func TestKillNeverSignalsSelfOrInit(t *testing.T) {
	self, parent := int32(os.Getpid()), int32(os.Getppid())
	const ancestor int32 = 1 << 29
	now := time.Now()
	for _, pid := range []int32{-1, 0, 1, self, parent, ancestor} {
		t.Run(fmt.Sprint(pid), func(t *testing.T) {
			procs := []model.Proc{{PID: self, PPID: parent, StartTime: now}, {PID: parent, PPID: ancestor, StartTime: now}, {PID: ancestor, PPID: 1, StartTime: now}}
			if pid != self && pid != parent && pid != ancestor {
				procs = append(procs, model.Proc{PID: pid, StartTime: now})
			}
			idx := derive.NewProcIndex(procs)
			var expect []int32
			for _, proc := range idx.Descendants(pid) {
				expect = append(expect, proc.PID)
			}
			p := snapshotPlatform{procs: procs}
			calls := 0
			r, err := tree(context.Background(), p, model.Orphan{RootPID: pid, StartTime: now}, expect, true, time.Millisecond, func(int, syscall.Signal) error { calls++; return nil })
			if err != nil || !r.Aborted || !strings.Contains(r.Reason, "protected PID") || calls != 0 {
				t.Fatalf("%+v %v signals=%d", r, err, calls)
			}
		})
	}
	// A protected descendant must abort the entire operation before any signal.
	p := snapshotPlatform{procs: []model.Proc{{PID: 100, StartTime: now}, {PID: self, PPID: 100, StartTime: now}}}
	calls := 0
	r, err := tree(context.Background(), p, model.Orphan{RootPID: 100, StartTime: now}, []int32{self}, true, time.Millisecond, func(int, syscall.Signal) error { calls++; return nil })
	if err != nil || !r.Aborted || calls != 0 {
		t.Fatalf("%+v %v signals=%d", r, err, calls)
	}
}

func TestKillValidationAndSignalOrder(t *testing.T) {
	now := time.Now()
	// Synthetic PIDs are only passed to a recording signal function.
	procs := []model.Proc{{PID: 100, StartTime: now}, {PID: 300, PPID: 100, StartTime: now}, {PID: 200, PPID: 300, StartTime: now}}
	for _, force := range []bool{false, true} {
		t.Run(fmt.Sprint(force), func(t *testing.T) {
			p := snapshotPlatform{procs: procs, alive: func(int32, time.Time) bool { return true }}
			var term, escalation []int32
			r, err := tree(context.Background(), p, model.Orphan{RootPID: 100, StartTime: now}, []int32{300, 200, 300}, force, time.Millisecond, func(pid int, sig syscall.Signal) error {
				if sig == syscall.SIGTERM {
					term = append(term, int32(pid))
				} else if sig == syscall.SIGKILL {
					escalation = append(escalation, int32(pid))
				} else {
					t.Fatalf("unexpected signal %v", sig)
				}
				return nil
			})
			want := []int32{200, 300, 100}
			if err != nil || r.Aborted || !slices.Equal(term, want) || !slices.Equal(r.Signaled, want) || !slices.Equal(r.Survivors, want) {
				t.Fatalf("%+v %v term=%v", r, err, term)
			}
			if force && !slices.Equal(escalation, want) || !force && len(escalation) != 0 {
				t.Fatalf("SIGKILL=%v force=%v", escalation, force)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		orphan model.Orphan
		expect []int32
	}{
		{"root-missing", model.Orphan{RootPID: 400, StartTime: now}, nil},
		{"start-mismatch", model.Orphan{RootPID: 100, StartTime: now.Add(2 * time.Second)}, []int32{200, 300}},
		{"set-mismatch", model.Orphan{RootPID: 100, StartTime: now}, []int32{200, 400}},
		{"young", model.Orphan{RootPID: 100, StartTime: now, Young: true}, []int32{200, 300}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := snapshotPlatform{procs: procs}
			calls := 0
			r, err := tree(context.Background(), p, tc.orphan, tc.expect, true, time.Millisecond, func(int, syscall.Signal) error { calls++; return nil })
			if err != nil || !r.Aborted || r.Reason == "" || calls != 0 {
				t.Fatalf("%+v %v calls=%d", r, err, calls)
			}
		})
	}
}
