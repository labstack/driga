package collector

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/spool"
)

type socketPlatform struct {
	*fakePlatform
	peers     map[int32]bool
	requested []int32
}

func (p *socketPlatform) ExternalPeers(_ context.Context, pids []int32) (map[int32]bool, error) {
	p.requested = append([]int32(nil), pids...)
	return p.peers, nil
}

func TestPassVerifiesOrphansViaSocketProber(t *testing.T) {
	d, st, p, _ := setup(t)
	p.procs = append(p.procs, model.Proc{PID: 200, PPID: 1, StartTime: d.Now().Add(-time.Hour), Cmdline: "claude mcp serve", Argv: []string{"claude", "mcp", "serve"}}, model.Proc{PID: 201, PPID: 200, StartTime: d.Now().Add(-time.Hour), Cmdline: "worker", Argv: []string{"worker"}})
	probe := &socketPlatform{fakePlatform: p, peers: map[int32]bool{200: false, 201: true}}
	d.Plat = probe
	pass(t, d, st, Plan{Procs: true, Sockets: true})
	os, err := st.Orphans()
	if err != nil || len(os) != 0 || !reflect.DeepEqual(probe.requested, []int32{200, 201}) {
		t.Fatalf("connected candidate: %+v, pids=%v, err=%v", os, probe.requested, err)
	}
	probe.peers[201] = false
	for i := 0; i < 2; i++ {
		pass(t, d, st, Plan{Procs: true, Sockets: true})
		os, err = st.Orphans()
		if err != nil || len(os) != 1 || os[0].Verified != (i == 1) {
			t.Fatalf("idle pass %d: %+v %v", i, os, err)
		}
	}
	d.Plat = p
	pass(t, d, st, Plan{Procs: true, Sockets: true})
	os, err = st.Orphans()
	if err != nil || len(os) != 1 || os[0].Verified {
		t.Fatalf("missing probe verified: %+v %v", os, err)
	}
}

func TestSnapshotSkipsSocketProbe(t *testing.T) {
	d, _, p, _ := setup(t)
	p.procs = append(p.procs, model.Proc{PID: 200, PPID: 1, StartTime: d.Now().Add(-time.Hour), Cmdline: "claude mcp serve", Argv: []string{"claude", "mcp", "serve"}})
	probe := &socketPlatform{fakePlatform: p}
	d.Plat = probe
	st, err := Snapshot(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if probe.requested != nil {
		t.Fatalf("snapshot probed sockets for %v", probe.requested)
	}
	os, err := st.Orphans()
	if err != nil || len(os) != 1 || os[0].Verified {
		t.Fatalf("want one unverified candidate, got %+v %v", os, err)
	}
}

// A pass with a fresh process scan never needs the stored rows, so an
// unreadable stored row must not fail it: the per-pass reread is skipped.
func TestSocketProbeExcludesLiveHelpers(t *testing.T) {
	d, st, p, a := setup(t)
	p.procs = append(p.procs,
		model.Proc{PID: 200, PPID: 1, StartTime: d.Now().Add(-time.Hour), Cmdline: "claude mcp serve", Argv: []string{"claude", "mcp", "serve"}},
		model.Proc{PID: 300, PPID: 1, StartTime: d.Now().Add(-time.Hour), Cmdline: "claude mcp serve", Argv: []string{"claude", "mcp", "serve"}},
	)
	a.obs = append(a.obs, model.SessionObs{Adapter: "claude", ExternalID: "live-helper", PID: 300, ProcStart: p.procs[len(p.procs)-1].StartTime, RegistryStatus: "busy", LastActivityAt: d.Now()})
	probe := &socketPlatform{fakePlatform: p, peers: map[int32]bool{200: true}}
	d.Plat = probe
	pass(t, d, st, Plan{Procs: true, Sockets: true})
	if !reflect.DeepEqual(probe.requested, []int32{200}) {
		t.Fatalf("probe set changed external-peer ownership: %v", probe.requested)
	}
	if err := spool.Append(d.SpoolDir, model.HookEvent{Adapter: "claude", SessionID: "live-helper", TS: d.Now().Add(time.Second), Event: "SessionEnd"}); err != nil {
		t.Fatal(err)
	}
	pass(t, d, st, Plan{Procs: true, Sockets: true})
	if !reflect.DeepEqual(probe.requested, []int32{200, 300}) {
		t.Fatalf("same-pass SessionEnd was not used for probe selection: %v", probe.requested)
	}

}
