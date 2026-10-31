package kill

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/signatures"
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

var _ platform.SocketProber = seqProber{}

func (p seqProber) ExternalPeers(_ context.Context, pids []int32) (map[int32]model.PeerState, error) {
	result := make(map[int32]model.PeerState)
	if !p.probe {
		for _, pid := range pids {
			result[pid] = model.PeerNone
		}
	}
	for pid, state := range platform.FixturePeers(p.peers) {
		result[pid] = state
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

func TestVerifyIdleBudgetsBackgroundCPUPerProcess(t *testing.T) {
	// Each process ticks just under the background budget over the window.
	o, snaps := verifyFixture(1.02)
	snaps[1][0].CPUSeconds += 0.02
	ok, reason, err := VerifyIdle(context.Background(), seqProber{&seqPlatform{snaps: snaps}}, o, []int32{101}, 400*time.Millisecond)
	if err != nil || !ok {
		t.Fatalf("background ticks rejected: %v %q", err, reason)
	}
	o, snaps = verifyFixture(1.1)
	if ok, _, err := VerifyIdle(context.Background(), seqProber{&seqPlatform{snaps: snaps}}, o, []int32{101}, 400*time.Millisecond); err != nil || ok {
		t.Fatalf("busy child accepted: %v %v", ok, err)
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

type listenerProber struct {
	*seqPlatform
	states map[int32]model.PeerState
}

func (p listenerProber) ExternalPeers(context.Context, []int32) (map[int32]model.PeerState, error) {
	return p.states, nil
}

// Under a broker root, listeners and system-service sockets pass once the
// classifier sees its IdleWithoutClients signature; without one they do not.
func TestVerifyIdleListenerNeedsSignature(t *testing.T) {
	sigs, err := signatures.Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	broker := func() (model.Orphan, [][]model.Proc) {
		o, snaps := verifyFixture(1)
		for i := range snaps {
			snaps[i][0].Cmdline = "node /plugin/app-server-broker.mjs"
			snaps[i][0].Argv = []string{"node", "/plugin/app-server-broker.mjs"}
		}
		return o, snaps
	}
	states := map[int32]model.PeerState{100: model.PeerListening, 101: model.PeerNone}
	o, snaps := broker()
	if ok, reason, err := VerifyIdle(context.Background(), listenerProber{&seqPlatform{snaps: snaps}, states}, o, []int32{101}, time.Millisecond); err != nil || ok || reason != "pid 100 has a listening socket" {
		t.Fatalf("unclassified listener accepted: %v %q %v", ok, reason, err)
	}
	o, snaps = broker()
	classified := WithClassifier(derive.NewClassifier(sigs, 2))
	if ok, reason, err := VerifyIdle(context.Background(), listenerProber{&seqPlatform{snaps: snaps}, states}, o, []int32{101}, time.Millisecond, classified); err != nil || !ok {
		t.Fatalf("idle broker listener rejected: %v %q", err, reason)
	}
	o, snaps = broker()
	states[101] = model.PeerSystem
	if ok, reason, err := VerifyIdle(context.Background(), listenerProber{&seqPlatform{snaps: snaps}, states}, o, []int32{101}, time.Millisecond, classified); err != nil || !ok {
		t.Fatalf("child's system-service socket rejected: %v %q", err, reason)
	}
	o, snaps = broker()
	states[101] = model.PeerConnected
	if ok, reason, err := VerifyIdle(context.Background(), listenerProber{&seqPlatform{snaps: snaps}, states}, o, []int32{101}, time.Millisecond, classified); err != nil || ok || reason != "pid 101 has a connection outside the tree" {
		t.Fatalf("child client accepted: %v %q %v", ok, reason, err)
	}
	o, snaps = verifyFixture(1)
	states[101] = model.PeerNone
	if ok, _, err := VerifyIdle(context.Background(), listenerProber{&seqPlatform{snaps: snaps}, states}, o, []int32{101}, time.Millisecond, classified); err != nil || ok {
		t.Fatalf("unmarked root listener accepted: %v %v", ok, err)
	}
}

// Each process stays under its own budget, but together they are working.
func TestVerifyIdleRejectsBusyTree(t *testing.T) {
	start := time.Unix(1700000000, 0)
	var before, after []model.Proc
	var expect []int32
	before = append(before, model.Proc{PID: 100, PPID: 1, StartTime: start, CPUSeconds: 1})
	after = append(after, model.Proc{PID: 100, PPID: 1, StartTime: start, CPUSeconds: 1})
	for i := int32(101); i <= 110; i++ {
		expect = append(expect, i)
		before = append(before, model.Proc{PID: i, PPID: 100, StartTime: start, CPUSeconds: 1})
		// 25ms each is under the 30ms per-process budget; 250ms in all is
		// over the tree's 110ms.
		after = append(after, model.Proc{PID: i, PPID: 100, StartTime: start, CPUSeconds: 1.025})
	}
	o := model.Orphan{ID: "100:1700000000", RootPID: 100, StartTime: start, PIDs: append([]int32{100}, expect...)}
	ok, reason, err := VerifyIdle(context.Background(), seqProber{&seqPlatform{snaps: [][]model.Proc{before, after}}}, o, expect, 400*time.Millisecond)
	if err != nil || ok || !strings.HasPrefix(reason, "tree used") {
		t.Fatalf("busy tree accepted: %v %q %v", ok, reason, err)
	}
}
