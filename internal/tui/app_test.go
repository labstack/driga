package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/labstack/driga/internal/adapter"
	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/kill"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/spool"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
	"github.com/labstack/driga/internal/viewer"
)

type fakeSource struct {
	snap    Snapshot
	version int64
	loads   int
	err     error
}

func (s *fakeSource) Load(context.Context) (Snapshot, error)     { s.loads++; return s.snap, s.err }
func (s *fakeSource) DataVersion(context.Context) (int64, error) { return s.version, nil }

func fixture() *fakeSource {
	return &fakeSource{version: 1, snap: Snapshot{
		Envelope:  view.Envelope{Mode: "daemon", ObservedAt: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)},
		BoardData: view.BoardSnapshot{Nodes: []view.FlowNode{{ID: 1, Adapter: "claude", Label: "working", Title: "test title", Project: "repo", Cwd: "/tmp/repo with spaces"}}},
		Hostname:  "test-host", Summary: view.SummaryView{Agents: 1},
		Agents:       view.AgentsView{Rows: []view.AgentRow{{ID: 1, Adapter: "claude", Label: "working", Title: "test title", Project: "repo"}}},
		Inbox:        view.InboxView{Rows: []view.InboxRow{{InboxItem: model.InboxItem{ID: "event-1", SessionID: 1, Kind: "finished", Summary: "approve change"}}}},
		Orphans:      view.OrphansView{Rows: []view.OrphanRow{{Orphan: model.Orphan{ID: "100:1", RootPID: 100, PIDs: []int32{100, 101}, RSSTotal: 1024, Verified: true, Evidence: []string{"detached helper"}}}}},
		AgentDetails: map[int64]view.AgentView{1: {Session: model.Session{ID: 1, SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "external-1", PID: 30, Cwd: "/tmp/repo with spaces"}, Evidence: "hook notification"}, Resume: "claude --resume external-1"}},
	}}
}
func setup(t *testing.T, s *fakeSource, opts Options) tea.Model {
	t.Helper()
	if opts.DataDir == "" {
		opts.DataDir = t.TempDir()
	}
	opts.NoColor = true
	if opts.Now == nil {
		opts.Now = func() time.Time { return s.snap.ObservedAt }
	}
	m := New(s, opts)
	return execute(t, m, m.Init())
}
func press(m tea.Model, code rune) (tea.Model, tea.Cmd) {
	msg := tea.KeyPressMsg{Code: code}
	if code <= unicode.MaxRune && !unicode.IsControl(code) {
		msg.Text = string(code)
	}
	return m.Update(msg)
}
func key(m tea.Model, code rune) tea.Model { m, _ = press(m, code); return m }
func execute(t *testing.T, m tea.Model, cmd tea.Cmd) tea.Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("missing action command")
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		// The poll command comes first; leave the real-time tick to Tea.
		return execute(t, m, batch[0])
	}
	m, _ = m.Update(msg)
	return m
}

func TestTabSwitching(t *testing.T) {
	m := setup(t, fixture(), Options{}).(*app)
	for _, tc := range []struct {
		key   rune
		tab   int
		label string
	}{{'0', flowTab, "0 Flow"}, {'1', 1, "1 Projects"}, {'2', 4, "2 Machines"}} {
		m.Update(tea.KeyPressMsg{Code: tc.key})
		if m.tab != tc.tab || !strings.Contains(m.tabBar(), tc.label) {
			t.Fatal(m.View().Content)
		}
	}
	for _, key := range []rune{'3', '4', '5', '6', '7', '8'} {
		m.Update(tea.KeyPressMsg{Code: key})
		if m.tab != 4 {
			t.Fatal("removed tab key navigated", key)
		}
	}
	if strings.Contains(m.tabBar(), "Inbox") || strings.Contains(m.tabBar(), "Chat") {
		t.Fatal(m.tabBar())
	}
}
func TestDetailDoesNotAck(t *testing.T) {
	calls := 0
	m := setup(t, fixture(), Options{Spool: func(model.HookEvent) error { calls++; return nil }})
	m = key(key(m, '0'), 'h')
	m = key(m, tea.KeyEnter)
	if calls != 0 || !strings.Contains(m.View().Content, "approve change") || !strings.Contains(m.View().Content, "hook notification") {
		t.Fatal(m.View().Content, calls)
	}
	m = key(m, tea.KeyEscape)
	if m.(*app).tab != 2 || !strings.Contains(m.View().Content, "0 Flow") {
		t.Fatal(m.View().Content)
	}
}
func TestAckWritesSpool(t *testing.T) {
	for _, k := range []rune{'x', ' '} {
		t.Run(string(k), func(t *testing.T) {
			dir := t.TempDir()
			s := fixture()
			m := setup(t, s, Options{Spool: func(e model.HookEvent) error { return spool.Append(dir, e) }})
			m = key(key(m, '0'), 'h')
			m, cmd := press(m, k)
			before, _, _ := spool.Drain(dir, nil)
			if len(before) != 0 {
				t.Fatal("IO ran in Update")
			}
			m = execute(t, m, cmd)
			events, _, err := spool.Drain(dir, nil)
			if err != nil || len(events) != 1 || events[0].Event != "ack" || events[0].Message != "event-1" {
				t.Fatal(events, err)
			}
			if !strings.Contains(m.View().Content, "driga inbox ack event-1") {
				t.Fatal(m.View().Content)
			}
		})
	}
}
func TestKillNeedsConfirmAndRefusesYoung(t *testing.T) {
	calls := 0
	var expect []int32
	opts := Options{Kill: func(_ context.Context, o model.Orphan, pids []int32, force bool) (kill.Result, error) {
		calls++
		expect = pids
		if force {
			t.Fatal("unexpected force")
		}
		return kill.Result{Signaled: []int32{o.RootPID}}, nil
	}}
	m := setup(t, fixture(), opts)
	m = key(key(m, '2'), 'p')
	m = key(m, 'k')
	if calls != 0 || !strings.Contains(m.View().Content, "[y/N]") || !strings.Contains(m.View().Content, "101") {
		t.Fatal(m.View().Content, calls)
	}
	m = key(m, 'n')
	m = key(m, 'y')
	if calls != 0 {
		t.Fatal("cancelled kill ran")
	}
	m = key(m, 'k')
	m, cmd := press(m, 'y')
	if calls != 0 {
		t.Fatal("kill ran in Update")
	}
	m = execute(t, m, cmd)
	if calls != 1 || !reflect.DeepEqual(expect, []int32{101}) || !strings.Contains(m.View().Content, "driga orphans kill 100:1") {
		t.Fatal(m.View().Content, calls, expect)
	}
	s := fixture()
	s.snap.Orphans.Rows[0].Young = true
	m = setup(t, s, opts)
	m = key(key(m, '2'), 'p')
	m = key(m, 'k')
	if !strings.Contains(m.View().Content, "too young to kill") || strings.Contains(m.View().Content, "[y/N]") {
		t.Fatal(m.View().Content)
	}
	m = key(m, 'y')
	if calls != 1 {
		t.Fatal("young kill ran")
	}
}

func TestKillRefusesUnverified(t *testing.T) {
	for _, action := range []rune{'k', 'K'} {
		s := fixture()
		s.snap.Orphans.Rows[0].Verified = false
		m := setup(t, s, Options{Kill: func(context.Context, model.Orphan, []int32, bool) (kill.Result, error) {
			t.Fatal("unverified candidate was killed")
			return kill.Result{}, nil
		}})
		m = key(key(m, '2'), 'p')
		m = key(m, action)
		if !strings.Contains(m.View().Content, "unverified — run: driga orphans kill") || strings.Contains(m.View().Content, "[y/N]") {
			t.Fatal(m.View().Content)
		}
		m, cmd := press(m, 'y')
		if cmd != nil {
			execute(t, m, cmd)
		}
	}
}
func TestReloadOnlyOnDataVersionChange(t *testing.T) {
	s := fixture()
	m := setup(t, s, Options{})
	m = key(key(m, '0'), 's')
	poll := func() { t.Helper(); var cmd tea.Cmd; m, cmd = m.Update(tickMsg{}); m = execute(t, m, cmd) }
	poll()
	if s.loads != 1 {
		t.Fatal(s.loads)
	}
	s.version++
	s.snap.Agents.Rows[0].Title = "changed title"
	poll()
	if s.loads != 2 || !strings.Contains(m.View().Content, "changed title") {
		t.Fatal(s.loads, m.View().Content)
	}
	poll()
	if s.loads != 2 {
		t.Fatal(s.loads)
	}
	s.version++
	s.err = errors.New("read failed")
	poll()
	s.err = nil
	poll()
	if s.loads != 4 {
		t.Fatal("failed reload must retry", s.loads)
	}
}
func TestProjectHidesEmptyTabs(t *testing.T) {
	s := fixture()
	s.snap.Orphans.Rows = nil
	m := setup(t, s, Options{Project: "/tmp/repo"})
	text := m.(*app).tabBar()
	if strings.Contains(text, "2 Projects") || strings.Contains(text, "4 Orphans") || !strings.Contains(text, "0 Flow") {
		t.Fatal(text)
	}
	m = key(key(m, '2'), 'p')
	if m.(*app).tab == 3 {
		t.Fatal(m.View().Content)
	}
}
func TestTmuxKeyHiddenWithoutTmux(t *testing.T) {
	for _, ancestor := range []bool{false, true} {
		t.Run(map[bool]string{false: "outside", true: "inside"}[ancestor], func(t *testing.T) {
			s := fixture()
			s.snap.Procs = []model.Proc{{PID: 30, PPID: 20}, {PID: 20, PPID: 10, Cmdline: "shell", Argv: []string{"shell"}}, {PID: 10, Cmdline: "/usr/bin/tmux new-session -s dev", Argv: []string{"/usr/bin/tmux", "new-session", "-s", "dev"}}}
			if !ancestor {
				s.snap.Procs[0].PPID = 1
			}
			var args []string
			m := setup(t, s, Options{Exec: func(cmd []string) error { args = cmd; return nil }})
			if strings.Contains(m.View().Content, "a attach tmux") != ancestor {
				t.Fatal(m.View().Content)
			}
			m, cmd := press(m, 'a')
			if ancestor {
				m = execute(t, m, cmd)
				if !reflect.DeepEqual(args, []string{"tmux", "attach", "-t", "dev"}) {
					t.Fatal(args)
				}
			} else if cmd != nil {
				t.Fatal("attach offered outside tmux")
			}
		})
	}
}
func TestOpenCmdFromTermProgram(t *testing.T) {
	cwd := "/tmp/repo with spaces"
	for _, tt := range []struct {
		term string
		want []string
	}{{"Apple_Terminal", []string{"open", "-a", "Terminal", cwd}}, {"iTerm.app", []string{"open", "-a", "iTerm", cwd}}, {"ghostty", []string{"open", "-a", "Ghostty", cwd}}, {"WezTerm", []string{"wezterm", "start", "--cwd", cwd}}, {"unknown", []string{"open", cwd}}} {
		t.Run(tt.term, func(t *testing.T) {
			var args []string
			m := setup(t, fixture(), Options{TermProgram: tt.term, Exec: func(cmd []string) error { args = cmd; return nil }})
			m, cmd := press(m, 'o')
			m = execute(t, m, cmd)
			if !reflect.DeepEqual(args, tt.want) {
				t.Fatal(args, tt.want)
			}
		})
	}
	t.Run("template", func(t *testing.T) {
		var args []string
		m := setup(t, fixture(), Options{OpenCmd: "terminal --dir={cwd}", Exec: func(cmd []string) error { args = cmd; return nil }})
		m, cmd := press(m, 'o')
		execute(t, m, cmd)
		if !reflect.DeepEqual(args, []string{"terminal", "--dir=" + cwd}) {
			t.Fatal(args)
		}
	})
}
func TestHeartbeatTouched(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission-bit contract; Windows uses ACLs")
	}

	dir := filepath.Join(t.TempDir(), "data")
	m := setup(t, fixture(), Options{DataDir: dir})
	m, cmd := m.Update(tickMsg{})
	execute(t, m, cmd)
	info, err := os.Stat(filepath.Join(dir, "viewer.heartbeat"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	d, err := os.Stat(dir)
	if err != nil || d.Mode().Perm() != 0700 {
		t.Fatal(d, err)
	}
}

func TestHeartbeatWhileLoadPending(t *testing.T) {
	dir := t.TempDir()
	s := fixture()
	m := New(s, Options{DataDir: dir})
	_ = m.Init() // Leave the initial load pending, as with a slow source.
	m, cmd := m.Update(tickMsg{})
	execute(t, m, cmd)
	if s.loads != 0 {
		t.Fatal("overlapping load")
	}
	if _, err := os.Stat(filepath.Join(dir, "viewer.heartbeat")); err != nil {
		t.Fatal(err)
	}
}
func TestRendersAt80Columns(t *testing.T) {
	s := fixture()
	s.snap.Agents.Rows[0].Title = strings.Repeat("界é", 100) + "\x1b[31m\nline"
	s.snap.Freshness = map[string]view.Fresh{"procs": {Stale: true, AgeMS: 90000}}
	m := setup(t, s, Options{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for _, k := range []rune{'1', tea.KeyEnter, tea.KeyEscape, '2', '3', '4', 'k', tea.KeyEscape, '5', '?'} {
		m = key(m, k)
		text := m.View().Content
		for _, line := range strings.Split(text, "\n") {
			if lipgloss.Width(line) > 80 {
				t.Fatalf("line width %d: %q", lipgloss.Width(line), line)
			}
		}
		if strings.Contains(text, "\x1b") {
			t.Fatal("ANSI with NoColor")
		}
	}
}

func TestDetailActionKeepsDisplayedTargetAfterReload(t *testing.T) {
	s := fixture()
	var ack string
	m := setup(t, s, Options{Spool: func(e model.HookEvent) error { ack = e.Message; return nil }})
	m = key(key(m, '0'), 'h')
	m = key(m, tea.KeyEnter)
	s.version++
	s.snap.Inbox.Rows = []view.InboxRow{{InboxItem: model.InboxItem{ID: "new-event", SessionID: 1, Summary: "different item"}}}
	m, cmd := m.Update(tickMsg{})
	m = execute(t, m, cmd)
	m, cmd = press(m, 'x')
	execute(t, m, cmd)
	if ack != "event-1" {
		t.Fatalf("acked %q while showing event-1", ack)
	}
}

func TestFamiliesFilterSortAndCursors(t *testing.T) {
	s := fixture()
	s.snap.Agents.Rows[0].Children = []view.AgentRow{{ID: 2, Adapter: "codex", Label: "working", Title: "worker title"}}
	s.snap.Agents.Rows = append(s.snap.Agents.Rows, view.AgentRow{ID: 3, Adapter: "codex", Label: "waiting", Title: "other title", Mem: 100})
	m := setup(t, s, Options{})
	m = key(key(m, '0'), 's')
	if strings.Contains(m.View().Content, "worker title") {
		t.Fatal("family starts expanded")
	}
	// State sorting places waiting before working; move to the parent.
	m = key(m, 'j')
	m = key(m, tea.KeyRight)
	if !strings.Contains(m.View().Content, "worker title") {
		t.Fatal(m.View().Content)
	}
	cursor := m.(*app).cursors[0]
	m = key(key(m, '0'), 'h')
	m = key(key(m, '0'), 's')
	if m.(*app).cursors[0] != cursor {
		t.Fatal("lost cursor")
	}
	m = key(m, tea.KeyLeft)
	m = key(m, '/')
	for _, c := range "worker" {
		m = key(m, c)
	}
	m = key(m, tea.KeyEnter)
	if !strings.Contains(m.View().Content, "worker title") || strings.Contains(m.View().Content, "other title") {
		t.Fatal(m.View().Content)
	}
	m = key(m, '/')
	m = key(m, tea.KeyEscape)
	m = key(m, 's')
	m = key(m, 's')
	if !strings.Contains(m.View().Content, "sort: mem") {
		t.Fatal(m.View().Content)
	}
}

func TestStaleWithoutDatabaseWrite(t *testing.T) {
	s := fixture()
	now := s.snap.ObservedAt
	s.snap.Freshness = map[string]view.Fresh{"procs": {LastOKAt: now}}
	m := setup(t, s, Options{Now: func() time.Time { return now }})
	now = now.Add(time.Minute)
	m, cmd := m.Update(tickMsg{})
	m = execute(t, m, cmd)
	key(m, '?')
	if s.loads != 1 || !strings.Contains(m.View().Content, "(stale procs 1m0s)") || !strings.Contains(m.(*app).header(), "intake · attention") {
		t.Fatal(m.View().Content, s.loads)
	}
}

func TestStoreSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "driga.db")
	ctx := context.Background()
	now := time.Now()
	writer, err := store.Open(path, store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	var sessionID int64
	err = writer.Tx(ctx, func(tx *store.Tx) error {
		if err := tx.UpsertMachine(model.Machine{Hostname: "fixture-host"}); err != nil {
			return err
		}
		repo, err := tx.UpsertRepo(model.Repo{Name: "repo", Path: filepath.Join(dir, "repo")})
		if err != nil {
			return err
		}
		if _, err = tx.UpsertWorktree(model.Worktree{RepoID: repo, Path: filepath.Join(dir, "repo"), Branch: "main"}); err != nil {
			return err
		}
		sessionID, err = tx.UpsertSession(model.Session{RepoID: &repo, SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "fixture", Cwd: filepath.Join(dir, "repo"), StartedAt: now}, Label: "waiting"})
		if err != nil {
			return err
		}
		if err = tx.UpsertInboxItem(model.InboxItem{ID: "fixture-event", SessionID: sessionID, TS: now, Kind: "waiting"}); err != nil {
			return err
		}
		if err = tx.InsertSample(model.MachineSample{TS: now, Cores: 4}); err != nil {
			return err
		}
		return tx.SetSourceOK("procs", now)
	})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := store.Open(path, store.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	src := NewStoreSource(reader, dir, view.Filter{Project: filepath.Join(dir, "repo")})
	src.Now = func() time.Time { return now }
	snap, err := src.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Mode != "daemon" || snap.Hostname != "fixture-host" || len(snap.Agents.Rows) != 1 || len(snap.ProjectDetails) != 0 || len(snap.Inbox.Rows) != 1 {
		t.Fatalf("incomplete snapshot: %+v", snap)
	}
	if len(snap.Activity.Machines) != 1 || len(snap.Activity.Machines[0].Buckets) != 60 || snap.Activity.Machines[0].Buckets[59].Sample == nil || snap.Activity.Machines[0].Buckets[59].Sample.Cores != 4 {
		t.Fatalf("activity missing from snapshot: %+v", snap.Activity)
	}
	v, err := src.DataVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = writer.SetMeta("test", "changed"); err != nil {
		t.Fatal(err)
	}
	next, err := src.DataVersion(ctx)
	if err != nil || next == v {
		t.Fatal(v, next, err)
	}
}

type snapshotPlatform struct{}

func (snapshotPlatform) Procs(context.Context) ([]model.Proc, error) { return nil, nil }
func (snapshotPlatform) Machine(context.Context) (model.MachineSample, error) {
	return model.MachineSample{Cores: 2}, nil
}
func (snapshotPlatform) Cwds(context.Context, []int32) (map[int32]string, error) { return nil, nil }
func (snapshotPlatform) Alive(int32, time.Time) bool                             { return false }

func TestLiveSnapshotSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	s, err := NewLiveSnapshotSource(collector.Deps{Cfg: config.Default(), Plat: snapshotPlatform{}, SpoolDir: filepath.Join(home, "spool")}, home, view.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	s.Start(ctx)
	defer s.Close()
	first, err := s.DataVersion(ctx)
	if err != nil || first != 1 {
		t.Fatal(first, err)
	}
	snap, err := s.Load(ctx)
	if err != nil || snap.Mode != "snapshot" || snap.Machine.Sample.Cores != 2 {
		t.Fatal(snap, err)
	}
	if _, err = os.Stat(filepath.Join(home, "driga.db")); !os.IsNotExist(err) {
		t.Fatal("snapshot created persistent database", err)
	}
	timer := time.NewTicker(20 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatal("no background pass", ctx.Err())
		case <-timer.C:
			next, err := s.DataVersion(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if next > first {
				return
			}
		}
	}
}

func TestInitialSnapshotLoadingMessage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	s, err := NewLiveSnapshotSource(collector.Deps{Cfg: config.Default(), Plat: snapshotPlatform{}, SpoolDir: filepath.Join(home, "spool")}, home, view.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	m := New(s, Options{DataDir: home, NoColor: true})
	_ = m.Init() // Leave collection pending to inspect the first frame.
	if text := m.View().Content; !strings.Contains(text, "Collecting a snapshot (no collector running)…") || strings.Contains(text, "Loading…") {
		t.Fatal(text)
	}
	m = New(fixture(), Options{DataDir: home, NoColor: true})
	if text := m.View().Content; !strings.Contains(text, "Loading…") || strings.Contains(text, "Collecting a snapshot") {
		t.Fatal(text)
	}
}

type scopedTestAdapter struct{ obs []model.SessionObs }

func (scopedTestAdapter) Name() string             { return "test" }
func (scopedTestAdapter) TestedVersions() []string { return nil }
func (a scopedTestAdapter) Collect(context.Context, adapter.Env) ([]model.SessionObs, []adapter.Warning, error) {
	return a.obs, nil, nil
}

func TestProjectScopedCollection(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(home, "repo")
	other := filepath.Join(home, "other")
	for _, path := range []string{project, other} {
		if err := os.MkdirAll(filepath.Join(path, ".git"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	a := scopedTestAdapter{obs: []model.SessionObs{{ExternalID: "inside", Cwd: project}, {ExternalID: "outside", Cwd: other}}}
	d := collector.Deps{Cfg: config.Default(), Adapters: []adapter.Adapter{a}}
	d.Cfg.Roots = []string{home}
	scoped, err := viewer.ScopeDeps(d, project)
	if err != nil {
		t.Fatal(err)
	}
	obs, _, err := scoped.Adapters[0].Collect(context.Background(), adapter.Env{})
	if err != nil || len(obs) != 1 || obs[0].ExternalID != "inside" {
		t.Fatal(obs, err)
	}
	normalized, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	if len(scoped.Cfg.Roots) != 1 || scoped.Cfg.Roots[0] != normalized || scoped.Cfg.RootDepth != 0 {
		t.Fatal(scoped.Cfg)
	}
}

func TestTmuxRewrittenProcessTitle(t *testing.T) {
	s := fixture()
	s.snap.Procs = []model.Proc{{PID: 30, PPID: 20}, {PID: 20, PPID: 10}, {PID: 10, Cmdline: "tmux: server", Argv: []string{"tmux:", "server"}}}
	var args []string
	m := setup(t, s, Options{LookupTmux: func(_ context.Context, pids []int32) (string, error) { return tmuxSession("$1\t500\n$2\t20\n", pids) }, Exec: func(cmd []string) error { args = cmd; return nil }})
	if !strings.Contains(m.View().Content, "a attach tmux") {
		t.Fatal(m.View().Content)
	}
	m, cmd := press(m, 'a')
	execute(t, m, cmd)
	if !reflect.DeepEqual(args, []string{"tmux", "attach", "-t", "$2"}) {
		t.Fatal(args)
	}
}

func TestForceRequiresSecondConfirmation(t *testing.T) {
	calls := 0
	m := setup(t, fixture(), Options{Kill: func(_ context.Context, _ model.Orphan, _ []int32, force bool) (kill.Result, error) {
		if !force {
			t.Fatal("force not set")
		}
		calls++
		return kill.Result{}, nil
	}})
	m = key(key(m, '2'), 'p')
	m = key(m, 'K')
	m, cmd := press(m, 'y')
	if cmd != nil || calls != 0 || !strings.Contains(m.View().Content, "Really SIGKILL") {
		t.Fatal(m.View().Content, calls)
	}
	m = key(m, tea.KeyEscape)
	m = key(m, 'K')
	m = key(m, 'y')
	m, cmd = press(m, 'y')
	execute(t, m, cmd)
	if calls != 1 {
		t.Fatal(calls)
	}
}
