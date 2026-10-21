package kill

import (
	"context"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
)

// seqPlatform returns successive process snapshots, one per Procs call.
type seqPlatform struct {
	snaps [][]model.Proc
	calls int
	peers map[int32]bool
	probe bool
}

func (p *seqPlatform) Procs(context.Context) ([]model.Proc, error) {
	s := p.snaps[min(p.calls, len(p.snaps)-1)]
	p.calls++
	return s, nil
}
func (p *seqPlatform) Machine(context.Context) (model.MachineSample, error) {
	return model.MachineSample{}, nil
}
func (p *seqPlatform) Cwds(context.Context, []int32) (map[int32]string, error) { return nil, nil }
func (p *seqPlatform) Alive(int32, time.Time) bool                             { return true }

type seqProber struct{ *seqPlatform }

func (p seqProber) ExternalPeers(_ context.Context, pids []int32) (map[int32]bool, error) {
	result := make(map[int32]bool)
	if !p.probe {
		for _, pid := range pids {
			result[pid] = false
		}
	}
	for pid, active := range p.peers {
		result[pid] = active
	}
	return result, nil
}

func verifyFixture(cpuAfter float64) (model.Orphan, [][]model.Proc) {
	start := time.Unix(1700000000, 0)
	o := model.Orphan{ID: "100:1700000000", RootPID: 100, StartTime: start, PIDs: []int32{100, 101}}
	before := []model.Proc{{PID: 100, PPID: 1, StartTime: start, CPUSeconds: 2}, {PID: 101, PPID: 100, StartTime: start, CPUSeconds: 1}}
	after := []model.Proc{{PID: 100, PPID: 1, StartTime: start, CPUSeconds: 2}, {PID: 101, PPID: 100, StartTime: start, CPUSeconds: cpuAfter}}
	return o, [][]model.Proc{before, after}
}

func TestVerifyIdleAcceptsQuietDisconnectedTree(t *testing.T) {
	o, snaps := verifyFixture(1)
	p := seqProber{&seqPlatform{snaps: snaps}}
	ok, reason, err := VerifyIdle(context.Background(), p, o, []int32{101}, time.Millisecond)
	if err != nil || !ok {
		t.Fatalf("quiet tree rejected: %v %q", err, reason)
	}
}

func TestVerifyIdleRejectsCPUGrowth(t *testing.T) {
	o, snaps := verifyFixture(1.5)
	ok, reason, err := VerifyIdle(context.Background(), seqProber{&seqPlatform{snaps: snaps}}, o, []int32{101}, time.Millisecond)
	if err != nil || ok || reason == "" {
		t.Fatalf("busy tree accepted: %v %v %q", ok, err, reason)
	}
}

func TestVerifyIdleRejectsExternalPeers(t *testing.T) {
	o, snaps := verifyFixture(1)
	p := seqProber{&seqPlatform{snaps: snaps, peers: map[int32]bool{101: true}}}
	if ok, _, err := VerifyIdle(context.Background(), p, o, []int32{101}, time.Millisecond); err != nil || ok {
		t.Fatalf("connected tree accepted: %v %v", ok, err)
	}
}

func TestVerifyIdleRejectsTreeChange(t *testing.T) {
	o, snaps := verifyFixture(1)
	snaps[1] = append(snaps[1], model.Proc{PID: 102, PPID: 100, StartTime: o.StartTime})
	if ok, _, err := VerifyIdle(context.Background(), seqProber{&seqPlatform{snaps: snaps}}, o, []int32{101}, time.Millisecond); err != nil || ok {
		t.Fatalf("changed tree accepted: %v %v", ok, err)
	}
}

func TestVerifyIdleRequiresSocketProber(t *testing.T) {
	o, snaps := verifyFixture(1)
	var p platform.Platform = &seqPlatform{snaps: snaps}
	if ok, _, err := VerifyIdle(context.Background(), p, o, []int32{101}, time.Millisecond); err != nil || ok {
		t.Fatalf("unprobed tree accepted: %v %v", ok, err)
	}
}

func TestWindowsVerifyIdleRejectsReusedChildAndMissingProbe(t *testing.T) {
	for _, reused := range []bool{false, true} {
		o, snaps := verifyFixture(1)
		if reused {
			snaps[1][1].StartTime = snaps[1][1].StartTime.Add(time.Second)
		}
		p := seqProber{&seqPlatform{snaps: snaps, probe: true}}
		if ok, reason, err := verifyIdle(context.Background(), p, o, []int32{101}, time.Millisecond, true); err != nil || ok || reason == "" {
			t.Fatalf("reused=%t: ok=%t reason=%q err=%v", reused, ok, reason, err)
		}
	}
}
