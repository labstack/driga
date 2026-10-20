package collector

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/adapter"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/gitscan"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/signatures"
	"github.com/labstack/driga/internal/spool"
	"github.com/labstack/driga/internal/store"
)

type fakePlatform struct {
	procs                       []model.Proc
	dead                        bool
	procErr, machineErr, cwdErr error
	cwds                        map[int32]string
	cwdPIDs                     []int32
	cwdCalls                    int
	procCalls                   int
	procCollect                 func(int) []model.Proc
}

func (f *fakePlatform) Procs(context.Context) ([]model.Proc, error) {
	f.procCalls++
	if f.procCollect != nil {
		return f.procCollect(f.procCalls), f.procErr
	}
	return append([]model.Proc(nil), f.procs...), f.procErr
}
func (f *fakePlatform) Machine(context.Context) (model.MachineSample, error) {
	return model.MachineSample{MemTotal: 1000, MemUsed: 400, Cores: 4}, f.machineErr
}
func (f *fakePlatform) Cwds(_ context.Context, pids []int32) (map[int32]string, error) {
	f.cwdCalls++
	f.cwdPIDs = append([]int32(nil), pids...)
	return f.cwds, f.cwdErr
}
func (f *fakePlatform) Alive(pid int32, start time.Time) bool {
	if f.dead {
		return false
	}
	for _, p := range f.procs {
		if p.PID == pid && platform.SameStart(p.StartTime, start) { // like the real platform: registry lag allowed
			return true
		}
	}
	return false
}

type fakeAdapter struct {
	name    string
	obs     []model.SessionObs
	err     error
	envs    []adapter.Env
	collect func(context.Context)
}

func (f *fakeAdapter) Name() string             { return f.name }
func (f *fakeAdapter) TestedVersions() []string { return nil }
func (f *fakeAdapter) Collect(ctx context.Context, env adapter.Env) ([]model.SessionObs, []adapter.Warning, error) {
	f.envs = append(f.envs, env)
	if f.collect != nil {
		f.collect(ctx)
	}
	return f.obs, nil, f.err
}

func setup(t *testing.T) (Deps, *store.Store, *fakePlatform, *fakeAdapter) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("COPILOT_HOME", filepath.Join(home, ".copilot"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("DRIGA_DATA_DIR", t.TempDir())
	t.Setenv("DRIGA_SPOOL_DIR", t.TempDir())
	t.Setenv("DRIGA_CONFIG", filepath.Join(home, "config.toml"))
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	p := &fakePlatform{procs: []model.Proc{{PID: 101, PPID: 2, StartTime: now.Add(-time.Hour), Cmdline: "claude", Cwd: home, CPUSeconds: 2}}}
	a := &fakeAdapter{name: "claude", obs: []model.SessionObs{{Adapter: "claude", ExternalID: "one", PID: 101, ProcStart: p.procs[0].StartTime, Cwd: home, RegistryStatus: "busy", StartedAt: p.procs[0].StartTime, LastActivityAt: now.Add(-time.Second)}}}
	sigs, err := signatures.Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	d := Deps{Cfg: config.Default(), Plat: p, Adapters: []adapter.Adapter{a}, Sigs: sigs, Git: gitscan.NewScanner(4), SpoolDir: config.SpoolDir(), Now: func() time.Time { return now }, State: NewState()}
	return d, st, p, a
}

func pass(t *testing.T, d Deps, st *store.Store, p Plan) {
	t.Helper()
	if _, err := Pass(context.Background(), d, st, p); err != nil {
		t.Fatal(err)
	}
}
func sessions(t *testing.T, st *store.Store) []model.Session {
	t.Helper()
	s, err := st.Sessions(store.SessionFilter{IncludeEnded: true})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func inbox(t *testing.T, st *store.Store) []model.InboxItem {
	t.Helper()
	i, err := st.Inbox(time.Time{}, true)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func TestPassWritesSessionsProcsSamples(t *testing.T) {
	d, st, p, a := setup(t)
	a.obs[0].FilesTouched = []model.FileTouch{{Path: "file.go", TS: d.Now()}}
	pass(t, d, st, Plan{Procs: true, Machine: true, Cwds: true})
	ss := sessions(t, st)
	if len(ss) != 1 || ss[0].Label != "working" {
		t.Fatalf("sessions: %+v", ss)
	}
	ps, err := st.Procs()
	if err != nil || len(ps) != 1 || ps[0].Agent != "claude" || ps[0].CPUPercent != 0 {
		t.Fatalf("procs: %+v %v", ps, err)
	}
	sample, err := st.LatestSample()
	if err != nil || sample.MemUsed != 400 || !sample.TS.Equal(d.Now()) {
		t.Fatalf("sample: %+v %v", sample, err)
	}
	m, err := st.Machine()
	if err != nil || m.ID != st.MachineID() || m.Cores != 4 {
		t.Fatalf("machine: %+v %v", m, err)
	}
	files, err := st.SessionFiles(time.Time{})
	if err != nil || len(files[ss[0].ID]) != 1 {
		t.Fatalf("files: %+v %v", files, err)
	}
	if len(p.cwdPIDs) != 1 || p.cwdPIDs[0] != 101 {
		t.Fatalf("cwd pids: %v", p.cwdPIDs)
	}
	next := d.Now().Add(5 * time.Second)
	d.Now = func() time.Time { return next }
	p.procs[0].CPUSeconds += 2
	a.obs[0].FilesTouched = nil
	pass(t, d, st, Plan{Procs: true})
	ps, err = st.Procs()
	if err != nil || ps[0].CPUPercent != 40 {
		t.Fatalf("CPU delta: %+v %v", ps, err)
	}
}

func TestPassSourceErrorRecordedAndContinues(t *testing.T) {
	d, st, _, a := setup(t)
	a.err = errors.New("source failed")
	pass(t, d, st, Plan{Procs: true, Machine: true})
	sources, err := st.Sources()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range sources {
		if s.Name == "claude" {
			found = s.LastError == "source failed" && s.LastErrorAt.Equal(d.Now())
		}
	}
	ps, err := st.Procs()
	if !found || err != nil || len(ps) != 1 {
		t.Fatalf("sources=%+v procs=%+v err=%v", sources, ps, err)
	}
}

func TestSnapshotDoesNotTouchDisk(t *testing.T) {
	d, _, _, _ := setup(t)
	if err := spool.Append(d.SpoolDir, model.HookEvent{TS: d.Now(), Adapter: "claude", SessionID: "one", Event: "Notification"}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(d.SpoolDir)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(d.SpoolDir, entries[0].Name())
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	st, err := Snapshot(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if len(sessions(t, st)) != 1 {
		t.Fatal("empty snapshot")
	}
	data, err := os.ReadDir(config.DataDir())
	if err != nil || len(data) != 0 {
		t.Fatalf("data dir: %v %v", data, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatalf("spool changed: %v", err)
	}
	entries, err = os.ReadDir(d.SpoolDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("spool entries: %v %v", entries, err)
	}
}

func TestFirstPassBaseline(t *testing.T) {
	d, st, _, a := setup(t)
	a.obs[0].Completions = []model.CompletionEvent{{ID: "old", TS: d.Now().Add(-time.Hour)}}
	pass(t, d, st, Plan{Procs: true})
	items := inbox(t, st)
	if len(items) != 1 || items[0].AckedAt == nil || !a.envs[0].Baseline {
		t.Fatalf("baseline: %+v", items)
	}
	base := d.Now()
	next := base.Add(time.Minute)
	d.Now = func() time.Time { return next }
	a.obs[0].Completions = []model.CompletionEvent{{ID: "new", TS: next}}
	pass(t, d, st, Plan{})
	items = inbox(t, st)
	if len(items) != 2 || items[0].AckedAt != nil || a.envs[1].Baseline {
		t.Fatalf("new completion: %+v", items)
	}
	value, err := st.Meta("baseline_at")
	if err != nil || value != base.Format(time.RFC3339Nano) {
		t.Fatalf("baseline meta: %q %v", value, err)
	}
}

func TestAckViaSpool(t *testing.T) {
	d, st, _, a := setup(t)
	if err := st.SetMeta("baseline_at", d.Now().Add(-time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	a.obs[0].Completions = []model.CompletionEvent{{ID: "done", TS: d.Now()}}
	pass(t, d, st, Plan{Procs: true})
	id := inbox(t, st)[0].ID
	for _, key := range []string{id, "missing"} {
		if err := spool.Append(d.SpoolDir, model.HookEvent{TS: d.Now(), Event: "ack", Message: key}); err != nil {
			t.Fatal(err)
		}
	}
	pass(t, d, st, Plan{})
	if inbox(t, st)[0].AckedAt == nil {
		t.Fatal("ack not applied")
	}
	for _, key := range []string{id, "missing"} {
		if v, err := st.Meta("ack:" + key); err != nil || v != d.Now().Format(time.RFC3339Nano) {
			t.Fatalf("receipt %q: %q %v", key, v, err)
		}
	}
	entries, _ := os.ReadDir(d.SpoolDir)
	if off, ok, err := st.GetOffset("spool/" + entries[0].Name()); err != nil || !ok || off.Pos == 0 {
		t.Fatalf("offset: %+v %v %v", off, ok, err)
	}
}

func TestCodexPidLinking(t *testing.T) {
	d, st, p, a := setup(t)
	a.name = "codex"
	start := d.Now().Add(-time.Minute)
	p.procs = []model.Proc{{PID: 10, StartTime: start.Add(-time.Second), Cmdline: "codex", Cwd: t.TempDir()}, {PID: 11, StartTime: start.Add(3 * time.Second), Cmdline: "codex exec", Cwd: ""}, {PID: 12, StartTime: start.Add(6 * time.Second), Cmdline: "codex", Cwd: ""}, {PID: 13, StartTime: start, Cmdline: "codex app-server", Cwd: ""}}
	cwd := p.procs[0].Cwd
	for i := 1; i < len(p.procs); i++ {
		p.procs[i].Cwd = cwd
	}
	a.obs = []model.SessionObs{{Adapter: "codex", ExternalID: "older", Cwd: cwd, StartedAt: start.Add(-time.Second), TurnOpen: true}, {Adapter: "codex", ExternalID: "newer", Cwd: cwd, StartedAt: start, TurnOpen: true}, {Adapter: "codex", ExternalID: "hosted", Cwd: cwd, StartedAt: start, Hosted: true}}
	pass(t, d, st, Plan{Procs: true})
	got := map[string]int32{}
	for _, s := range sessions(t, st) {
		got[s.ExternalID] = s.PID
	}
	if got["newer"] != 11 || got["older"] != 10 || got["hosted"] != 0 {
		t.Fatalf("links: %v", got)
	}
}

func TestDeadSessionGetsEndedAt(t *testing.T) {
	d, st, p, a := setup(t)
	pass(t, d, st, Plan{Procs: true})
	if sessions(t, st)[0].EndedAt != nil {
		t.Fatal("live session ended")
	}
	p.dead = true
	a.obs = nil
	now := d.Now().Add(time.Minute)
	d.Now = func() time.Time { return now }
	pass(t, d, st, Plan{Procs: true})
	s := sessions(t, st)[0]
	if s.PID != 0 || s.EndedAt == nil || !s.EndedAt.Equal(now) || s.Label != "dead" {
		t.Fatalf("dead: %+v", s)
	}
	pass(t, d, st, Plan{})
	if !sessions(t, st)[0].EndedAt.Equal(now) {
		t.Fatal("ended time drifted")
	}
}

func TestLabelsUseHookEvents(t *testing.T) {
	d, st, _, _ := setup(t)
	if err := spool.Append(d.SpoolDir, model.HookEvent{TS: d.Now(), Adapter: "claude", SessionID: "one", Event: "Notification"}); err != nil {
		t.Fatal(err)
	}
	pass(t, d, st, Plan{Procs: true})
	if s := sessions(t, st)[0]; s.Label != "waiting" {
		t.Fatalf("label: %+v", s)
	}
	pass(t, d, st, Plan{})
	if s := sessions(t, st)[0]; s.Label != "waiting" {
		t.Fatalf("persisted hook label: %+v", s)
	}
}

func TestSessionRepoMapping(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git missing")
	}
	d, st, _, a := setup(t)
	repo := t.TempDir()
	cmd := exec.Command("git", "init", repo)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %s %v", out, err)
	}
	cwd := filepath.Join(repo, "sub", "dir")
	if err := os.MkdirAll(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	a.obs[0].Cwd = cwd
	pass(t, d, st, Plan{Procs: true, Git: true, GitFull: true})
	s := sessions(t, st)[0]
	if s.RepoID == nil || s.WorktreeID == nil {
		t.Fatalf("mapping: %+v", s)
	}
	pass(t, d, st, Plan{})
	s = sessions(t, st)[0]
	if s.RepoID == nil || s.WorktreeID == nil {
		t.Fatal("mapping lost without git scan")
	}
}

func TestOrphanStored(t *testing.T) {
	d, st, p, _ := setup(t)
	p.procs = append(p.procs, model.Proc{PID: 200, PPID: 1, StartTime: d.Now().Add(-time.Hour), Cmdline: "claude mcp serve", RSS: 123})
	pass(t, d, st, Plan{Procs: true})
	orphans, err := st.Orphans()
	if err != nil || len(orphans) != 1 || orphans[0].RootPID != 200 || orphans[0].RSSTotal != 123 {
		t.Fatalf("orphans: %+v %v", orphans, err)
	}
}

func TestPassRollbackKeepsStateAndOffsets(t *testing.T) {
	d, st, _, a := setup(t)
	if err := spool.Append(d.SpoolDir, model.HookEvent{TS: d.Now(), Adapter: "claude", SessionID: "one", Event: "Notification"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.collect = func(context.Context) { cancel() }
	if _, err := Pass(ctx, d, st, Plan{Procs: true, Machine: true}); err == nil {
		t.Fatal("canceled transaction committed")
	}
	if len(d.State.prev) != 0 || !d.State.prevAt.IsZero() {
		t.Fatal("rolled-back CPU state leaked")
	}
	if _, err := st.Meta("baseline_at"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("baseline survived rollback: %v", err)
	}
	ps, err := st.Procs()
	if err != nil || len(ps) != 0 {
		t.Fatalf("procs survived rollback: %v %v", ps, err)
	}
	entries, _ := os.ReadDir(d.SpoolDir)
	if _, ok, err := st.GetOffset("spool/" + entries[0].Name()); err != nil || ok {
		t.Fatalf("offset survived rollback: %v %v", ok, err)
	}
	a.collect = nil
	pass(t, d, st, Plan{Procs: true})
	if s := sessions(t, st)[0]; s.Label != "waiting" {
		t.Fatalf("hook lost on retry: %+v", s)
	}
}

func TestFailedSourcesRetainPriorData(t *testing.T) {
	d, st, p, _ := setup(t)
	pass(t, d, st, Plan{Procs: true, Machine: true})
	p.procErr = errors.New("process source failed")
	p.machineErr = errors.New("machine source failed")
	p.procs = nil
	now := d.Now().Add(time.Minute)
	d.Now = func() time.Time { return now }
	pass(t, d, st, Plan{Procs: true, Machine: true})
	ps, err := st.Procs()
	if err != nil || len(ps) != 1 {
		t.Fatalf("lost previous procs: %v %v", ps, err)
	}
	samples, err := st.Samples(time.Time{})
	if err != nil || len(samples) != 1 {
		t.Fatalf("bad samples: %v %v", samples, err)
	}
	sources, err := st.Sources()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sources {
		if s.Name == "procs" || s.Name == "machine" {
			if s.LastError == "" || !s.LastOKAt.Before(s.LastErrorAt) {
				t.Fatalf("freshness: %+v", s)
			}
		}
	}
}

func TestCwdCacheThrottleAndPidReuse(t *testing.T) {
	d, st, p, _ := setup(t)
	p.cwds = map[int32]string{101: "/old-cwd"}
	pass(t, d, st, Plan{Procs: true, Cwds: true})
	now := d.Now().Add(5 * time.Second)
	d.Now = func() time.Time { return now }
	pass(t, d, st, Plan{Procs: true, Cwds: true})
	ps, err := st.Procs()
	if err != nil || ps[0].Cwd != "/old-cwd" || p.cwdCalls != 1 {
		t.Fatalf("cwd cache: %+v %v calls=%d", ps, err, p.cwdCalls)
	}
	p.procs[0].StartTime = now
	p.procs[0].Cwd = "/new-cwd"
	pass(t, d, st, Plan{Procs: true, Cwds: true})
	ps, err = st.Procs()
	if err != nil || ps[0].Cwd != "/new-cwd" {
		t.Fatalf("reused pid got stale cwd: %+v %v", ps, err)
	}
	now = now.Add(time.Minute)
	p.cwds[101] = "/resolved-new-cwd"
	pass(t, d, st, Plan{Procs: true, Cwds: true})
	ps, err = st.Procs()
	if err != nil || ps[0].Cwd != "/resolved-new-cwd" || p.cwdCalls != 2 {
		t.Fatalf("cwd refresh: %+v %v calls=%d", ps, err, p.cwdCalls)
	}
}

func TestCodexLinksFreshCwds(t *testing.T) {
	d, st, p, a := setup(t)
	a.name = "codex"
	a.obs[0].Adapter = "codex"
	a.obs[0].PID = 0
	a.obs[0].RegistryStatus = ""
	p.procs[0].Cmdline = "codex"
	p.procs[0].Cwd = ""
	p.cwds = map[int32]string{101: a.obs[0].Cwd}
	pass(t, d, st, Plan{Procs: true, Cwds: true})
	if s := sessions(t, st)[0]; s.PID != 101 || s.EndedAt != nil {
		t.Fatalf("fresh cwd linking: %+v", s)
	}
}

func TestSnapshotSamplesCPU(t *testing.T) {
	d, _, p, a := setup(t)
	// Model a real CPU counter across the adapter work, rather than assuming
	// a fixed half-second sampling interval.
	a.collect = func(context.Context) { time.Sleep(10 * time.Millisecond) }
	started := time.Now()
	p.procCollect = func(n int) []model.Proc {
		ps := append([]model.Proc(nil), p.procs...)
		ps[0].CPUSeconds = time.Since(started).Seconds()
		return ps
	}
	st, err := Snapshot(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ps, err := st.Procs()
	if err != nil || len(ps) != 1 || ps[0].CPUPercent < 50 || ps[0].CPUPercent > 210 {
		t.Fatalf("snapshot CPU: %+v %v", ps, err)
	}
	if p.procCalls != 2 || len(d.State.prev) != 0 {
		t.Fatalf("snapshot sampling/state: calls=%d state=%v", p.procCalls, d.State.prev)
	}
}

func TestCapturedTextPolicy(t *testing.T) {
	for _, capture := range []string{"titles", "snippets"} {
		t.Run(capture, func(t *testing.T) {
			d, st, p, a := setup(t)
			d.Cfg.CaptureText = capture
			a.name = "codex"
			a.obs[0].Adapter = "codex"
			p.procs[0].Cmdline = "codex"
			secret := "ghp_verysecretvalue"
			text := "answer " + secret + strings.Repeat(" x", 250)
			a.obs[0].LastPrompt = text
			a.obs[0].Completions = []model.CompletionEvent{{ID: "done", TS: d.Now(), Text: text}}
			var repo int64
			if err := st.Tx(context.Background(), func(tx *store.Tx) (err error) {
				repo, err = tx.UpsertRepo(model.Repo{Path: a.obs[0].Cwd})
				if err != nil {
					return err
				}
				_, err = tx.UpsertWorktree(model.Worktree{RepoID: repo, Path: a.obs[0].Cwd})
				return err
			}); err != nil {
				t.Fatal(err)
			}
			pass(t, d, st, Plan{Procs: true})
			s := sessions(t, st)[0]
			items := inbox(t, st)
			handoffs, err := st.Handoffs(repo, 0)
			if err != nil {
				t.Fatal(err)
			}
			if capture == "titles" {
				if s.LastPrompt != "" || len(handoffs) != 0 || items[0].Summary != "turn finished" {
					t.Fatalf("text retained without opt-in: %+v %+v %+v", s, items, handoffs)
				}
			} else {
				if len(handoffs) != 1 {
					t.Fatalf("handoffs: %+v", handoffs)
				}
				for _, value := range []string{s.LastPrompt, items[0].Summary, handoffs[0].Text} {
					if strings.Contains(value, secret) || len([]rune(value)) > 200 {
						t.Fatalf("unscrubbed/unbounded text: %q", value)
					}
				}
			}
		})
	}
}
