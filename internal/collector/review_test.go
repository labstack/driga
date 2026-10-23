package collector

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/spool"
	"github.com/labstack/driga/internal/store"
)

func TestPassScansGitForResumedSession(t *testing.T) {
	d, st, _, a := setup(t)
	repo := t.TempDir()
	if out, err := exec.Command("git", "init", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %s %v", out, err)
	}
	old := d.Now().Add(-8 * 24 * time.Hour)
	a.obs[0].Cwd, a.obs[0].LastActivityAt = repo, old
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "one", Cwd: repo, LastActivityAt: old}, EndedAt: &old, Label: "dead"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	pass(t, d, st, Plan{Procs: true, Git: true, GitFull: true})
	if s := sessions(t, st)[0]; s.EndedAt != nil || s.RepoID == nil {
		t.Fatalf("resumed session omitted from git: %+v", s)
	}
}

func TestPassCollectsSourcesBeforeTransactionConcurrently(t *testing.T) {
	d, st, _, a := setup(t)
	b := &fakeAdapter{name: "codex"}
	d.Adapters = append(d.Adapters, b)
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	var mu sync.Mutex
	var readErr error
	collect := func(ctx context.Context) {
		readCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		_, err := st.DataVersion(readCtx)
		mu.Lock()
		if err != nil {
			readErr = err
		}
		mu.Unlock()
		ready <- struct{}{}
		<-release
	}
	a.collect, b.collect = collect, collect
	done := make(chan error, 1)
	go func() { _, err := Pass(context.Background(), d, st, Plan{Procs: true}); done <- err }()
	concurrent := true
	for i := 0; i < 2; i++ {
		select {
		case <-ready:
		case <-time.After(300 * time.Millisecond):
			concurrent = false
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !concurrent || readErr != nil {
		t.Fatalf("sources concurrent=%v, read outside transaction=%v", concurrent, readErr)
	}
}

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
	p.procs = append(p.procs, model.Proc{PID: 200, PPID: 1, StartTime: d.Now().Add(-time.Hour), Cmdline: "claude mcp serve"}, model.Proc{PID: 201, PPID: 200, StartTime: d.Now().Add(-time.Hour), Cmdline: "worker"})
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

func TestNewCodexSessionLinkedSamePass(t *testing.T) {
	d, st, p, a := setup(t)
	pass(t, d, st, Plan{Procs: true, Cwds: true})
	a.name = "codex"
	a.obs = []model.SessionObs{{Adapter: "codex", ExternalID: "new", Cwd: t.TempDir(), StartedAt: d.Now()}}
	p.procs = append(p.procs, model.Proc{PID: 202, PPID: 2, StartTime: d.Now(), Cmdline: "codex"})
	p.cwds = map[int32]string{202: a.obs[0].Cwd}
	pass(t, d, st, Plan{Procs: true, Sockets: true})
	for _, s := range sessions(t, st) {
		if s.ExternalID == "new" {
			if s.PID != 202 || s.EndedAt != nil {
				t.Fatalf("not linked in discovery pass: %+v", s)
			}
			return
		}
	}
	t.Fatal("new session missing")
}

func TestImmediateCwdLookupBoundedAndFair(t *testing.T) {
	d, st, p, a := setup(t)
	a.obs = nil
	p.procs = nil
	for i := int32(0); i < 205; i++ {
		p.procs = append(p.procs, model.Proc{PID: 1000 + i, PPID: 2, StartTime: d.Now(), Cmdline: "codex"})
	}
	seen := make(map[int32]bool)
	for range 2 {
		pass(t, d, st, Plan{Procs: true, Sockets: true})
		if len(p.cwdPIDs) > 200 {
			t.Fatalf("immediate lookup is unbounded: %d", len(p.cwdPIDs))
		}
		for _, pid := range p.cwdPIDs {
			seen[pid] = true
		}
	}
	if len(seen) != 205 {
		t.Fatalf("unresolved processes starved new lookups: %d", len(seen))
	}
}

func TestSnapshotDrainsOnlyRecentSpool(t *testing.T) {
	d, _, _, _ := setup(t)
	for _, age := range []time.Duration{0, 8 * 24 * time.Hour} {
		if err := spool.Append(d.SpoolDir, model.HookEvent{TS: d.Now().Add(-age), Adapter: "claude", SessionID: "one", Event: "Notification"}); err != nil {
			t.Fatal(err)
		}
	}
	st, err := Snapshot(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	events, err := st.LatestEvents(time.Time{})
	if err != nil || len(events) != 1 {
		t.Fatalf("snapshot history: %+v %v", events, err)
	}
	entries, _ := os.ReadDir(d.SpoolDir)
	if len(entries) != 2 {
		t.Fatal("snapshot pruned the spool")
	}
}

func TestPassUpsertsOnlyChangedSessions(t *testing.T) {
	d, _, _, a := setup(t)
	path := filepath.Join(t.TempDir(), "db")
	st, err := store.Open(path, store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE writes(n INTEGER); INSERT INTO writes VALUES(0); CREATE TRIGGER count_updates AFTER UPDATE ON sessions BEGIN UPDATE writes SET n=n+1; END;`)
	if err != nil {
		t.Fatal(err)
	}
	pass(t, d, st, Plan{Procs: true, Sockets: true})
	if _, err = db.Exec("UPDATE writes SET n=0"); err != nil {
		t.Fatal(err)
	}
	pass(t, d, st, Plan{})
	var n int
	if err := db.QueryRow("SELECT n FROM writes").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("unchanged session updated %d times", n)
	}
	a.obs[0].Title = "changed"
	pass(t, d, st, Plan{})
	if err := db.QueryRow("SELECT n FROM writes").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("changed session writes=%d", n)
	}
}

func TestDaemonDrainsSpoolSoonAfterAck(t *testing.T) {
	d, _, p, a := setup(t)
	dir := config.DataDir()
	// An existing day file catches append detection as well as creation.
	if err := spool.Append(d.SpoolDir, model.HookEvent{TS: d.Now(), Event: "ack", Message: "initial"}); err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{}, 1)
	a.collect = func(context.Context) { ready <- struct{}{} }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, d, dir) }()
	var stopped sync.Once
	stop := func() {
		stopped.Do(func() {
			cancel()
			if err := <-done; err != nil {
				t.Error(err)
			}
		})
	}
	defer stop()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not start")
	}
	st, err := store.Open(filepath.Join(dir, "driga.db"), store.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := st.Meta("ack:initial"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("initial pass not committed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	started := time.Now()
	if err := spool.Append(d.SpoolDir, model.HookEvent{TS: d.Now(), Event: "ack", Message: "new"}); err != nil {
		t.Fatal(err)
	}
	for time.Since(started) < 1500*time.Millisecond {
		if _, err := st.Meta("ack:new"); err == nil {
			stop()
			if p.procCalls != 1 || len(a.envs) != 1 {
				t.Fatal("spool wake collected other sources")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("ack receipt not written within idle wake interval")
}

func TestSpoolOnlyPassPreservesHookInboxAndHandoff(t *testing.T) {
	d, st, _, _ := setup(t)
	d.Cfg.CaptureText = "snippets"
	pass(t, d, st, Plan{Procs: true, Sockets: true})
	var repo int64
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		var err error
		repo, err = tx.UpsertRepo(model.Repo{Path: t.TempDir()})
		if err != nil {
			return err
		}
		ss, err := tx.Sessions(store.SessionFilter{})
		if err != nil {
			return err
		}
		ss[0].RepoID = &repo
		_, err = tx.UpsertSession(ss[0])
		return err
	}); err != nil {
		t.Fatal(err)
	}
	now := d.Now().Add(time.Second)
	d.Now = func() time.Time { return now }
	if err := spool.Append(d.SpoolDir, model.HookEvent{TS: now, Adapter: "claude", SessionID: "one", Event: "Stop", Message: "completed"}); err != nil {
		t.Fatal(err)
	}
	if err := drainOnly(context.Background(), d, st); err != nil {
		t.Fatal(err)
	}
	items := inbox(t, st)
	if len(items) != 1 || items[0].Summary != "completed" {
		t.Fatalf("spool-only completion lost: %+v", items)
	}
	handoffs, err := st.Handoffs(repo, 0)
	if err != nil || len(handoffs) != 1 || handoffs[0].Text != "completed" {
		t.Fatalf("spool-only handoff lost: %+v %v", handoffs, err)
	}
}

// A single snapshot pass can never verify an orphan, so it must not pay for
// the socket probe (it dominated snapshot latency on a loaded machine).
func TestSnapshotSkipsSocketProbe(t *testing.T) {
	d, _, p, _ := setup(t)
	p.procs = append(p.procs, model.Proc{PID: 200, PPID: 1, StartTime: d.Now().Add(-time.Hour), Cmdline: "claude mcp serve"})
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
func TestPassWithFreshProcsSkipsStoredProcs(t *testing.T) {
	d, _, _, _ := setup(t)
	path := filepath.Join(t.TempDir(), "db")
	st, err := store.Open(path, store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO procs (machine_id,pid,ppid,start_time,rss,cpu_seconds,cpu_percent,cmdline,exe,role,agent,session_id,cwd) VALUES (?,'unreadable',0,0,0,0,0,'','','','',0,'')`, st.MachineID()); err != nil {
		t.Fatal(err)
	}
	pass(t, d, st, Plan{Procs: true})
	ps, err := st.Procs()
	if err != nil || len(ps) != 1 || ps[0].PID != 101 {
		t.Fatalf("procs: %+v %v", ps, err)
	}
	// Later passes without a scan reuse the rows this run wrote, not the DB.
	if _, err := db.Exec(`UPDATE procs SET pid='unreadable'`); err != nil {
		t.Fatal(err)
	}
	pass(t, d, st, Plan{})
	if ss := sessions(t, st); len(ss) != 1 || ss[0].PID != 101 || ss[0].Label != "working" {
		t.Fatalf("sessions after unscanned pass: %+v", ss)
	}
}

func TestSocketProbeExcludesLiveHelpers(t *testing.T) {
	d, st, p, a := setup(t)
	p.procs = append(p.procs,
		model.Proc{PID: 200, PPID: 1, StartTime: d.Now().Add(-time.Hour), Cmdline: "claude mcp serve"},
		model.Proc{PID: 300, PPID: 1, StartTime: d.Now().Add(-time.Hour), Cmdline: "claude mcp serve"},
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
