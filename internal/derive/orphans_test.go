package derive

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/signatures"
)

func TestOrphanFirstSightingUnverified(t *testing.T) {
	procs, sigs, now := orphanFixture(t)
	got := FindOrphans(NewProcIndex(procs), nil, sigs, now, 0, nil)
	if len(got) != 1 {
		t.Fatal(got)
	}
	b, err := json.Marshal(got[0])
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(b, &fields); err != nil {
		t.Fatal(err)
	}
	if v, ok := fields["verified"]; !ok || v != false {
		t.Fatalf("first sighting must explicitly be unverified: %s", b)
	}
}

func TestOrphanSecondIdlePassVerified(t *testing.T) {
	procs, sigs, now := orphanFixture(t)
	opts := WithExternalPeers(map[int32]bool{10: false, 11: false, 12: false})
	first := FindOrphans(NewProcIndex(procs), nil, sigs, now, 0, nil, opts)
	if len(first) != 1 || first[0].Verified {
		t.Fatal(first)
	}
	prev := map[string]model.Orphan{first[0].ID: first[0]}
	second := FindOrphans(NewProcIndex(procs), nil, sigs, now.Add(time.Minute), 0, prev, opts)
	if len(second) != 1 || !second[0].Verified {
		t.Fatalf("second idle pass: %+v", second)
	}
}

func TestOrphanWithExternalPeersIsNotOrphan(t *testing.T) {
	procs, sigs, now := orphanFixture(t)
	for _, p := range procs {
		t.Run(fmt.Sprint(p.PID), func(t *testing.T) {
			if got := FindOrphans(NewProcIndex(procs), nil, sigs, now, 0, nil, WithExternalPeers(map[int32]bool{p.PID: true})); len(got) != 0 {
				t.Fatal(got)
			}
		})
	}
}

func TestOrphanVerificationRequiresConsecutiveProbedIdlePasses(t *testing.T) {
	procs, sigs, now := orphanFixture(t)
	clear := WithExternalPeers(map[int32]bool{10: false, 11: false, 12: false})
	first := FindOrphans(NewProcIndex(procs), nil, sigs, now, 0, nil, clear)
	prev := map[string]model.Orphan{first[0].ID: first[0]}
	// An unknown probe result invalidates the observation window, even if CPU
	// is still zero. A later successful probe starts a new window.
	unknown := FindOrphans(NewProcIndex(procs), nil, sigs, now.Add(time.Minute), 0, prev)
	if len(unknown) != 1 || unknown[0].Verified {
		t.Fatal(unknown)
	}
	prev = map[string]model.Orphan{unknown[0].ID: unknown[0]}
	again := FindOrphans(NewProcIndex(procs), nil, sigs, now.Add(2*time.Minute), 0, prev, clear)
	if len(again) != 1 || again[0].Verified {
		t.Fatalf("unprobed previous pass verified: %+v", again)
	}
	// A partially probed tree cannot establish an idle observation either.
	partial := FindOrphans(NewProcIndex(procs), nil, sigs, now.Add(time.Minute), 0, map[string]model.Orphan{first[0].ID: first[0]}, WithExternalPeers(map[int32]bool{10: false}))
	if len(partial) != 1 || partial[0].Verified {
		t.Fatal(partial)
	}
	// A newly added descendant has no preceding idle observation.
	procs = append(procs, model.Proc{PID: 13, PPID: 10, StartTime: now})
	changed := FindOrphans(NewProcIndex(procs), nil, sigs, now.Add(time.Minute), 0, map[string]model.Orphan{first[0].ID: first[0]}, WithExternalPeers(map[int32]bool{10: false, 11: false, 12: false, 13: false}))
	if len(changed) != 1 || changed[0].Verified {
		t.Fatal(changed)
	}
}

func orphanFixture(t *testing.T) ([]model.Proc, *signatures.Set, time.Time) {
	t.Helper()
	now := time.Unix(20000, 0).UTC()
	sigs, err := signatures.Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	return []model.Proc{
		{PID: 10, PPID: 1, Cmdline: "node /plugin/app-server-broker.mjs", StartTime: now.Add(-2 * time.Hour), RSS: 100, Cwd: "/repo"},
		{PID: 11, PPID: 10, Cmdline: "codex app-server", StartTime: now.Add(-time.Hour), RSS: 200},
		{PID: 12, PPID: 11, Cmdline: "claude mcp serve", StartTime: now.Add(-time.Hour), RSS: 300},
	}, sigs, now
}

func TestCodexAppServerDaemonNeverOrphan(t *testing.T) {
	_, sigs, now := orphanFixture(t)
	const exe = "/Users/example/.codex/packages/app-server-daemon/releases/1.2.3/bin/codex"
	for _, cmd := range []string{exe + " app-server --remote-control", exe + " app-server daemon pid-update 123", "codex app-server"} {
		for _, classified := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/classified=%t", cmd, classified), func(t *testing.T) {
				root := model.Proc{PID: 10, PPID: 1, Cmdline: cmd, StartTime: now.Add(-time.Hour)}
				if classified {
					sig, _ := sigs.Match(cmd, "")
					root.Role = string(sig.Role)
				}
				procs := []model.Proc{root, {PID: 11, PPID: 10, Cmdline: "claude mcp serve", StartTime: root.StartTime}}
				got := FindOrphans(NewProcIndex(procs), nil, sigs, now, 0, nil, WithSpawnMemory(map[string]int64{"10:" + fmt.Sprint(root.StartTime.Unix()): 99, "11:" + fmt.Sprint(root.StartTime.Unix()): 99}))
				want := 0
				if cmd == "codex app-server" {
					want = 1
				}
				if len(got) != want {
					t.Fatalf("orphans = %+v; want %d", got, want)
				}
			})
		}
	}
}

func TestOrphanBrokerTree(t *testing.T) {
	procs, sigs, now := orphanFixture(t)
	got := FindOrphans(NewProcIndex(procs), nil, sigs, now, 10*time.Minute, nil)
	if len(got) != 1 {
		t.Fatal(got)
	}
	o := got[0]
	if o.RootPID != 10 || o.ProcCount != 3 || o.RSSTotal != 600 || o.ID != fmt.Sprintf("10:%d", procs[0].StartTime.Unix()) || o.Young || !o.FirstSeen.Equal(now) || !reflect.DeepEqual(o.PIDs, []int32{10, 11, 12}) {
		t.Fatal(o)
	}
	if !strings.Contains(strings.Join(o.Evidence, ";"), "ppid=1") || o.Signature == "" {
		t.Fatal(o)
	}
	prev := map[string]model.Orphan{o.ID: o}
	got = FindOrphans(NewProcIndex(procs), nil, sigs, now.Add(time.Minute), 10*time.Minute, prev)
	if len(got) != 1 || !got[0].FirstSeen.Equal(now) {
		t.Fatal(got)
	}
}

func TestOrphanYoung(t *testing.T) {
	procs, sigs, now := orphanFixture(t)
	procs[0].StartTime = now.Add(-5 * time.Minute)
	got := FindOrphans(NewProcIndex(procs), nil, sigs, now, 10*time.Minute, nil)
	if len(got) != 1 || !got[0].Young {
		t.Fatal(got)
	}
}

func TestNotOrphanWhenBusy(t *testing.T) {
	procs, sigs, now := orphanFixture(t)
	for i := range procs {
		p := append([]model.Proc(nil), procs...)
		p[i].CPUPercent = 0.1
		if got := FindOrphans(NewProcIndex(p), nil, sigs, now, 0, nil); len(got) != 0 {
			t.Fatal(got)
		}
	}
}

func TestNotOrphanAppBundle(t *testing.T) {
	procs, sigs, now := orphanFixture(t)
	procs = procs[:1]
	procs[0].Exe = "/Applications/Foo.app/Contents/Helpers/broker"
	main := model.Proc{PID: 20, PPID: 1, Exe: "/Applications/Foo.app/Contents/MacOS/Foo"}
	if got := FindOrphans(NewProcIndex(append(procs, main)), nil, sigs, now, 0, nil); len(got) != 0 {
		t.Fatal(got)
	}
	if got := FindOrphans(NewProcIndex(procs), nil, sigs, now, 0, nil); len(got) != 1 {
		t.Fatal(got)
	}
}

func TestNotOrphanUnmatched(t *testing.T) {
	procs, sigs, now := orphanFixture(t)
	procs = procs[:1]
	procs[0].Cmdline = "vim"
	if got := FindOrphans(NewProcIndex(procs), nil, sigs, now, 0, nil); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestOrphanSameDirIsEvidenceOnly(t *testing.T) {
	procs, sigs, now := orphanFixture(t)
	procs = append(procs, model.Proc{PID: 100, PPID: 1})
	got := FindOrphans(NewProcIndex(procs), []model.Session{familySession(1, 100)}, sigs, now, 0, nil)
	if len(got) != 1 || !strings.Contains(strings.Join(got[0].Evidence, ";"), "live session in same dir") {
		t.Fatal(got)
	}
}

func TestOrphanSpawnMemory(t *testing.T) {
	procs, sigs, now := orphanFixture(t)
	procs = []model.Proc{{PID: 100, PPID: 1}, {PID: 10, PPID: 100, Cmdline: "worker", StartTime: procs[0].StartTime}, {PID: 11, PPID: 10, Cmdline: "worker"}}
	sessions := []model.Session{familySession(7, 100)}
	memory := SpawnedBySession(NewProcIndex(procs), sessions)
	if !reflect.DeepEqual(memory, map[int32]int64{10: 7, 11: 7}) {
		t.Fatal(memory)
	}
	id := fmt.Sprintf("10:%d", procs[1].StartTime.Unix())
	opts := WithSpawnMemory(map[string]int64{id: memory[10]})
	if got := FindOrphans(NewProcIndex(procs), sessions, sigs, now, 0, nil, opts); len(got) != 0 {
		t.Fatal(got)
	}
	procs[1].PPID = 50 // recorded owner died without reparenting to pid 1
	got := FindOrphans(NewProcIndex(procs[1:]), nil, sigs, now, 0, nil, opts)
	if len(got) != 1 || !strings.Contains(strings.Join(got[0].Evidence, ";"), "spawned by session 7") {
		t.Fatal(got)
	}
	prev := map[string]model.Orphan{id: got[0]}
	if got := FindOrphans(NewProcIndex(procs[1:]), nil, sigs, now, 0, prev); len(got) != 1 {
		t.Fatal(got)
	}
	if got := FindOrphans(NewProcIndex(procs), sessions, sigs, now, 0, prev, opts); len(got) != 0 {
		t.Fatal("stale death evidence for a live owner", got)
	}
	procs[1].StartTime = now
	if got := FindOrphans(NewProcIndex(procs[1:]), nil, sigs, now, 0, prev, opts); len(got) != 0 {
		t.Fatal("PID reuse", got)
	}
}

func TestOrphanTopmostAndLiveSessionProtection(t *testing.T) {
	procs, sigs, now := orphanFixture(t)
	procs[1].PPID = 10
	childID := fmt.Sprintf("11:%d", procs[1].StartTime.Unix())
	got := FindOrphans(NewProcIndex(procs), nil, sigs, now, 0, nil, WithSpawnMemory(map[string]int64{childID: 99}))
	if len(got) != 1 || got[0].RootPID != 10 {
		t.Fatal(got)
	}
	if got := FindOrphans(NewProcIndex(procs[:1]), []model.Session{familySession(1, 10)}, sigs, now, 0, nil); len(got) != 0 {
		t.Fatal(got)
	}
	prev := map[string]model.Orphan{fmt.Sprintf("10:%d", procs[0].StartTime.Unix()): {Evidence: []string{"spawned by session"}}}
	procs[0].Cmdline = "worker"
	if got := FindOrphans(NewProcIndex(procs[:1]), nil, sigs, now, 0, prev); len(got) != 1 {
		t.Fatal(got)
	}
}

func TestOrphanReparentedToSystemdUser(t *testing.T) {
	procs, sigs, now := orphanFixture(t)
	procs[0].PPID = 500
	for _, pid := range []int32{0, -1, 499} {
		if got := FindOrphans(NewProcIndex(procs), nil, sigs, now, 10*time.Minute, nil, WithSubreaper(pid)); len(got) != 0 {
			t.Fatalf("unrelated subreaper %d: %+v", pid, got)
		}
	}
	opts := []OrphanOption{WithSubreaper(500), WithExternalPeers(map[int32]bool{10: false, 11: false, 12: false})}
	got := FindOrphans(NewProcIndex(procs), nil, sigs, now, 10*time.Minute, nil, opts...)
	if len(got) != 1 || got[0].ProcCount != 3 || got[0].Verified || !strings.Contains(strings.Join(got[0].Evidence, ";"), "reparented to systemd --user") {
		t.Fatalf("systemd orphan: %+v", got)
	}
	second := FindOrphans(NewProcIndex(procs), nil, sigs, now.Add(time.Minute), 10*time.Minute, map[string]model.Orphan{got[0].ID: got[0]}, opts...)
	if len(second) != 1 || !second[0].Verified {
		t.Fatal(second)
	}
	if got := FindOrphans(NewProcIndex(procs), []model.Session{familySession(1, 10)}, sigs, now, 0, nil, opts...); len(got) != 0 {
		t.Fatal("live session", got)
	}
}
