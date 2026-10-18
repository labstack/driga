package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/labstack/driga/internal/model"
)

func openTest(t *testing.T, path string, mode Mode) *Store {
	t.Helper()
	s, err := Open(path, mode)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}

func mustTx(t *testing.T, s *Store, fn func(*Tx) error) {
	t.Helper()
	if err := s.Tx(context.Background(), fn); err != nil {
		t.Fatal(err)
	}
}

func equal[T any](t *testing.T, want, got T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("(-want +got):\n%s", diff)
	}
}

func TestMigrateAndRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data ?#", "driga.db")
	s := openTest(t, path, ReadWrite)
	now := time.Date(2026, 10, 18, 12, 0, 0, 123456789, time.UTC)
	if len(s.MachineID()) != 32 {
		t.Fatalf("machine ID: %q", s.MachineID())
	}
	machine := model.Machine{ID: s.MachineID(), Hostname: "host", OS: "os", Arch: "arch", Cores: 8, MemTotal: 123, FirstSeen: now, LastSeen: now}
	repo := model.Repo{Path: "/repo", Name: "repo", RemoteURL: "remote"}
	wt := model.Worktree{Path: "/repo", Branch: "main", Head: "abc", Ahead: 1, Behind: 2, Dirty: 3, Untracked: 4, OldestDirtyMtime: now, LastCommitAt: now, LastCommitSubject: "subject", UpdatedAt: now}
	session := model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: "one", Origin: "cli", Version: "v1", PID: 12, ProcStart: now, Cwd: "/repo", Branch: "main", Title: "title", LastPrompt: "prompt", RegistryStatus: "alive", StartedAt: now, LastActivityAt: now, Hosted: true, TurnOpen: true, SourcePath: "/log"}, Label: "working", Heuristic: true, Evidence: "evidence"}
	proc := model.Proc{PID: 12, PPID: 1, StartTime: now, RSS: 123, CPUSeconds: 1.5, CPUPercent: 2.5, Cmdline: "agent", Exe: "/agent", Cwd: "/repo", Role: "host", Agent: "test"}
	sample := model.MachineSample{TS: now, MemUsed: 1, MemCompressed: 2, MemTotal: 3, SwapUsed: 4, SwapTotal: 5, DiskFree: 6, PressureFree: 7.5, Load1: 8, Load5: 9, Load15: 10, Cores: 11}
	orphan := model.Orphan{ID: "20:123", RootPID: 20, StartTime: now, Signature: "helper", ProcCount: 2, RSSTotal: 100, Cwd: "/repo", Young: true, Evidence: []string{"parent exited", "signature"}, FirstSeen: now, PIDs: []int32{20, 21}}
	offset := model.Offset{Inode: 42, Size: 100, Pos: 90, Mtime: now}
	event := model.Event{Adapter: "test", ExternalID: "one", TS: now, Kind: "Stop", Summary: "done"}
	gap := model.Gap{From: now.Add(-time.Minute), To: now}
	mustTx(t, s, func(tx *Tx) error {
		if err := tx.UpsertMachine(machine); err != nil {
			return err
		}
		var err error
		repo.ID, err = tx.UpsertRepo(repo)
		if err != nil {
			return err
		}
		wt.RepoID = repo.ID
		wt.ID, err = tx.UpsertWorktree(wt)
		if err != nil {
			return err
		}
		session.RepoID, session.WorktreeID = &repo.ID, &wt.ID
		session.ID, err = tx.UpsertSession(session)
		if err != nil {
			return err
		}
		proc.SessionID = session.ID
		if err = tx.ReplaceProcs([]model.Proc{proc}); err != nil {
			return err
		}
		if err = tx.InsertSample(sample); err != nil {
			return err
		}
		if err = tx.ReplaceOrphans([]model.Orphan{orphan}); err != nil {
			return err
		}
		if err = tx.SetOffset("/log", offset); err != nil {
			return err
		}
		if err = tx.SetSourceError("test", "failure", now.Add(-time.Second)); err != nil {
			return err
		}
		if err = tx.SetSourceOK("test", now); err != nil {
			return err
		}
		if err = tx.InsertEvent(event); err != nil {
			return err
		}
		if err = tx.InsertHandoff(session.ID, now, "done"); err != nil {
			return err
		}
		if err = tx.InsertGap(gap); err != nil {
			return err
		}
		return tx.SetMeta("baseline_at", "baseline")
	})
	gotMachine, err := s.Machine()
	equal(t, machine, gotMachine, err)
	gotRepos, err := s.Repos()
	equal(t, []model.Repo{repo}, gotRepos, err)
	gotWTs, err := s.Worktrees(repo.ID)
	equal(t, []model.Worktree{wt}, gotWTs, err)
	gotSession, err := s.Session(session.ID)
	equal(t, session, gotSession, err)
	gotSessions, err := s.Sessions(SessionFilter{RepoPath: repo.Path})
	equal(t, []model.Session{session}, gotSessions, err)
	gotProcs, err := s.Procs()
	equal(t, []model.Proc{proc}, gotProcs, err)
	gotSample, err := s.LatestSample()
	equal(t, sample, gotSample, err)
	gotSamples, err := s.Samples(now)
	equal(t, []model.MachineSample{sample}, gotSamples, err)
	gotOrphans, err := s.Orphans()
	equal(t, []model.Orphan{orphan}, gotOrphans, err)
	gotOffset, ok, err := s.GetOffset("/log")
	equal(t, offset, gotOffset, err)
	if !ok {
		t.Fatal("offset missing")
	}
	_, ok, err = s.GetOffset("/missing")
	if err != nil || ok {
		t.Fatalf("missing offset: %v, %v", ok, err)
	}
	gotEvents, err := s.Events("test", "one", now)
	equal(t, []model.Event{event}, gotEvents, err)
	gotEvents, err = s.LatestEvents(now)
	equal(t, []model.Event{event}, gotEvents, err)
	handoffs, err := s.Handoffs(repo.ID, 1)
	if err != nil || len(handoffs) != 1 || handoffs[0].Text != "done" || !handoffs[0].TS.Equal(now) {
		t.Fatalf("handoffs: %v, %v", handoffs, err)
	}
	sources, err := s.Sources()
	equal(t, []model.SourceStatus{{Name: "test", LastOKAt: now, LastError: "failure", LastErrorAt: now.Add(-time.Second)}}, sources, err)
	gaps, err := s.Gaps(gap.From)
	equal(t, []model.Gap{gap}, gaps, err)
	meta, err := s.Meta("baseline_at")
	equal(t, "baseline", meta, err)
	_, err = s.Session(-1)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing session: %v", err)
	}
	// Reopen proves migrations are repeatable and identity is persistent, including RO.
	reopened := openTest(t, path, ReadWrite)
	if reopened.MachineID() != s.MachineID() {
		t.Fatal("machine identity changed")
	}
	ro := openTest(t, path, ReadOnly)
	if ro.MachineID() != s.MachineID() {
		t.Fatal("reader identity differs")
	}
	var journal string
	var busy, syncMode int
	if err := s.db.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow("PRAGMA busy_timeout").Scan(&busy); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow("PRAGMA synchronous").Scan(&syncMode); err != nil {
		t.Fatal(err)
	}
	if journal != "wal" || busy != 5000 || syncMode != 1 {
		t.Fatalf("pragmas: %s %d %d", journal, busy, syncMode)
	}
	if s.db.Stats().MaxOpenConnections != 1 {
		t.Fatal("expected one connection")
	}
}

func TestReadOnlyCannotWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "driga.db")
	openTest(t, path, ReadWrite)
	ro := openTest(t, path, ReadOnly)
	if err := ro.Tx(context.Background(), func(tx *Tx) error { return tx.SetMeta("bad", "write") }); err == nil {
		t.Fatal("read-only write succeeded")
	}
	if err := ro.SetMeta("bad", "write"); err == nil {
		t.Fatal("direct read-only write succeeded")
	}
	missing := filepath.Join(t.TempDir(), "missing.db")
	if s, err := Open(missing, ReadOnly); err == nil {
		s.Close()
		t.Fatal("opened missing DB")
	}
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("RO created DB: %v", err)
	}
}

func TestMemoryModeIsolated(t *testing.T) {
	a, b := openTest(t, "", Memory), openTest(t, "", Memory)
	if a.MachineID() == b.MachineID() {
		t.Fatal("shared identity")
	}
	mustTx(t, a, func(tx *Tx) error {
		_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: "one"}})
		return err
	})
	sessions, err := b.Sessions(SessionFilter{})
	if err != nil || len(sessions) != 0 {
		t.Fatalf("shared rows: %v %v", sessions, err)
	}
	session, err := a.Session(1)
	if err != nil || !session.StartedAt.IsZero() || !session.ProcStart.IsZero() {
		t.Fatalf("zero times: %v %v", session, err)
	}
}

func TestInboxInsertIgnoreDedup(t *testing.T) {
	s := openTest(t, "", Memory)
	now := time.Now().UTC()
	item := model.InboxItem{ID: "finished:one", Kind: "finished", TS: now, Summary: "original"}
	mustTx(t, s, func(tx *Tx) error {
		id, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: "one"}})
		if err != nil {
			return err
		}
		item.SessionID = id
		if err = tx.UpsertInboxItem(item); err != nil {
			return err
		}
		duplicate := item
		duplicate.Summary = "replacement"
		return tx.UpsertInboxItem(duplicate)
	})
	items, err := s.Inbox(time.Time{}, false)
	equal(t, []model.InboxItem{item}, items, err)
	mustTx(t, s, func(tx *Tx) error {
		ok, err := tx.AckInbox(item.ID, now)
		if err != nil {
			return err
		}
		if !ok {
			t.Fatal("first ack false")
		}
		ok, err = tx.AckInbox(item.ID, now.Add(time.Second))
		if err != nil {
			return err
		}
		if ok {
			t.Fatal("second ack true")
		}
		ok, err = tx.AckInbox("missing", now)
		if err == nil && ok {
			t.Fatal("missing ack true")
		}
		return err
	})
	items, err = s.Inbox(time.Time{}, false)
	if err != nil || len(items) != 0 {
		t.Fatalf("acked visible: %v %v", items, err)
	}
	item.AckedAt = &now
	items, err = s.Inbox(now, true)
	equal(t, []model.InboxItem{item}, items, err)
}

func TestPruneRetention(t *testing.T) {
	s := openTest(t, "", Memory)
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	old, cutoff := now.Add(-31*24*time.Hour), now.Add(-30*24*time.Hour)
	var oldID, boundaryID, liveID int64
	mustTx(t, s, func(tx *Tx) error {
		var err error
		for i, ended := range []*time.Time{&old, &cutoff, nil} {
			id, e := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: fmt.Sprint(i)}, EndedAt: ended})
			if e != nil {
				return e
			}
			switch i {
			case 0:
				oldID = id
			case 1:
				boundaryID = id
			case 2:
				liveID = id
			}
			if err = tx.ReplaceSessionFiles(id, []model.FileTouch{{Path: "/file", TS: old}}); err != nil {
				return err
			}
			if err = tx.InsertHandoff(id, old, "done"); err != nil {
				return err
			}
		}
		for i, ts := range []time.Time{old, cutoff, now} {
			if err = tx.InsertEvent(model.Event{Adapter: "test", ExternalID: "live", TS: ts, Kind: "done"}); err != nil {
				return err
			}
			if err = tx.UpsertInboxItem(model.InboxItem{ID: fmt.Sprint(i), SessionID: liveID, TS: ts}); err != nil {
				return err
			}
			if err = tx.InsertGap(model.Gap{From: ts.Add(-time.Minute), To: ts}); err != nil {
				return err
			}
		}
		for _, ts := range []time.Time{now.Add(-8 * 24 * time.Hour), now.Add(-7 * 24 * time.Hour), now.Add(-48 * time.Hour), now.Add(-48*time.Hour + time.Minute), now.Add(-48*time.Hour + 5*time.Minute), now.Add(-24*time.Hour - time.Minute), now.Add(-24 * time.Hour), now.Add(-24*time.Hour + time.Minute), now} {
			if err = tx.InsertSample(model.MachineSample{TS: ts}); err != nil {
				return err
			}
		}
		return tx.Prune(now)
	})
	if _, err := s.Session(oldID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old session remains: %v", err)
	}
	if _, err := s.Session(boundaryID); err != nil {
		t.Fatal(err)
	}
	files, err := s.SessionFiles(time.Time{})
	if err != nil || len(files[oldID]) != 0 || len(files[liveID]) != 1 {
		t.Fatalf("files: %v %v", files, err)
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM handoffs WHERE session_id = ?", oldID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("handoffs remain: %d %v", count, err)
	}
	events, err := s.LatestEvents(time.Time{})
	if err != nil || len(events) != 2 {
		t.Fatalf("events: %v %v", events, err)
	}
	inbox, err := s.Inbox(time.Time{}, true)
	if err != nil || len(inbox) != 2 {
		t.Fatalf("inbox: %v %v", inbox, err)
	}
	gaps, err := s.Gaps(time.Time{})
	if err != nil || len(gaps) != 2 {
		t.Fatalf("gaps: %v %v", gaps, err)
	}
	samples, err := s.Samples(time.Time{})
	if err != nil || len(samples) != 7 {
		t.Fatalf("samples: %v %v", samples, err)
	}
	for _, v := range samples {
		if v.TS.Before(now.Add(-7*24*time.Hour)) || v.TS.Equal(now.Add(-48*time.Hour)) {
			t.Fatalf("wrong sample retained: %v", v.TS)
		}
	}
	// Repeated pruning must be idempotent.
	mustTx(t, s, func(tx *Tx) error { return tx.Prune(now) })
	again, err := s.Samples(time.Time{})
	equal(t, samples, again, err)
}

func TestFilePermissions(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprint(existing), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "data", "driga.db")
			if existing {
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, nil, 0644); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			}
			openTest(t, path, ReadWrite)
			for p, mode := range map[string]os.FileMode{filepath.Dir(path): 0700, path: 0600} {
				st, err := os.Stat(p)
				if err != nil {
					t.Fatal(err)
				}
				if st.Mode().Perm() != mode {
					t.Fatalf("%s mode %o, want %o", p, st.Mode().Perm(), mode)
				}
			}
		})
	}
}

func TestTxReadsAndRollback(t *testing.T) {
	s := openTest(t, "", Memory)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	sentinel := errors.New("rollback")
	err := s.Tx(ctx, func(tx *Tx) error {
		id, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: "one"}})
		if err != nil {
			return err
		}
		got, err := tx.Session(id)
		if err != nil {
			return err
		}
		if got.ExternalID != "one" {
			t.Fatal(got)
		}
		sessions, err := tx.Sessions(SessionFilter{})
		if err != nil {
			return err
		}
		if len(sessions) != 1 {
			t.Fatal(sessions)
		}
		if err := tx.SetMeta("transaction", "visible"); err != nil {
			return err
		}
		meta, err := tx.Meta("transaction")
		if err != nil {
			return err
		}
		if meta != "visible" {
			t.Fatal(meta)
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("transaction read failed or deadlocked: %v", err)
	}
	sessions, err := s.Sessions(SessionFilter{})
	if err != nil || len(sessions) != 0 {
		t.Fatalf("rollback: %v %v", sessions, err)
	}
}

func TestDataVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "driga.db")
	w := openTest(t, path, ReadWrite)
	r := openTest(t, path, ReadOnly)
	before, err := r.DataVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := w.SetMeta("other_connection", "changed"); err != nil {
		t.Fatal(err)
	}
	after, err := r.DataVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatalf("data version unchanged: %d", before)
	}
	stable, err := r.DataVersion(context.Background())
	equal(t, after, stable, err)
}

func TestUpsertAndFileMerge(t *testing.T) {
	s := openTest(t, "", Memory)
	now := time.Now().UTC()
	var repoID, wtID, sessionID int64
	mustTx(t, s, func(tx *Tx) error {
		var err error
		repoID, err = tx.UpsertRepo(model.Repo{Path: "/repo", Name: "old"})
		if err != nil {
			return err
		}
		id, err := tx.UpsertRepo(model.Repo{Path: "/repo", Name: "new"})
		if err != nil {
			return err
		}
		if id != repoID {
			t.Fatal("repo identity changed")
		}
		wtID, err = tx.UpsertWorktree(model.Worktree{RepoID: repoID, Path: "/repo", Branch: "old"})
		if err != nil {
			return err
		}
		id, err = tx.UpsertWorktree(model.Worktree{RepoID: repoID, Path: "/repo", Branch: "new"})
		if err != nil {
			return err
		}
		if id != wtID {
			t.Fatal("worktree identity changed")
		}
		session := model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: "one"}, RepoID: &repoID, WorktreeID: &wtID}
		sessionID, err = tx.UpsertSession(session)
		if err != nil {
			return err
		}
		session.Label, session.EndedAt, session.ParentID = "finished", &now, &sessionID
		id, err = tx.UpsertSession(session)
		if err != nil {
			return err
		}
		if id != sessionID {
			t.Fatal("session identity changed")
		}
		if err = tx.ReplaceSessionFiles(id, []model.FileTouch{{Path: "/a", TS: now}, {Path: "/b", TS: now.Add(-time.Hour)}}); err != nil {
			return err
		}
		if err = tx.ReplaceSessionFiles(id, []model.FileTouch{{Path: "/a", TS: now.Add(-time.Hour)}, {Path: "/b", TS: now.Add(time.Second)}, {Path: "/c"}}); err != nil {
			return err
		}
		return tx.ReplaceSessionFiles(id, nil)
	})
	files, err := s.SessionFiles(time.Time{})
	equal(t, []model.FileTouch{{Path: "/a", TS: now}, {Path: "/b", TS: now.Add(time.Second)}, {Path: "/c"}}, files[sessionID], err)
	files, err = s.SessionFiles(now)
	equal(t, []model.FileTouch{{Path: "/a", TS: now}, {Path: "/b", TS: now.Add(time.Second)}}, files[sessionID], err)
	sessions, err := s.Sessions(SessionFilter{})
	if err != nil || len(sessions) != 0 {
		t.Fatalf("ended visible: %v %v", sessions, err)
	}
	sessions, err = s.Sessions(SessionFilter{EndedSince: now})
	if err != nil || len(sessions) != 1 || sessions[0].ParentID == nil || *sessions[0].ParentID != sessionID {
		t.Fatalf("ended since: %v %v", sessions, err)
	}
	sessions, err = s.Sessions(SessionFilter{IncludeEnded: true, RepoPath: "/missing"})
	if err != nil || len(sessions) != 0 {
		t.Fatalf("repo filter: %v %v", sessions, err)
	}
}

func TestEventCap(t *testing.T) {
	s := openTest(t, "", Memory)
	base := time.Now().UTC()
	mustTx(t, s, func(tx *Tx) error {
		for i := range 205 {
			if err := tx.InsertEvent(model.Event{Adapter: "test", ExternalID: "one", TS: base.Add(time.Duration(i) * time.Second), Kind: "done"}); err != nil {
				return err
			}
		}
		return tx.InsertEvent(model.Event{Adapter: "other", ExternalID: "one", TS: base, Kind: "done"})
	})
	events, err := s.Events("test", "one", time.Time{})
	if err != nil || len(events) != 200 {
		t.Fatalf("cap: %d %v", len(events), err)
	}
	for _, ev := range events {
		if ev.TS.Before(base.Add(5 * time.Second)) {
			t.Fatalf("old event retained: %v", ev)
		}
	}
	other, err := s.Events("other", "one", time.Time{})
	if err != nil || len(other) != 1 {
		t.Fatalf("other adapter: %v %v", other, err)
	}
}
