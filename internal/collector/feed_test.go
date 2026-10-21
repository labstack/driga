package collector

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/feed"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/spool"
	"github.com/labstack/driga/internal/store"
)

func feedRows(t *testing.T, st *store.Store, seq int64) []store.FeedRow {
	t.Helper()
	epoch, _, _, err := st.FeedBounds()
	if err != nil {
		t.Fatal(err)
	}
	rows, err := st.FeedSince(epoch, seq, 0)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestPassAppendsOnlyChangedRows(t *testing.T) {
	d, st, _, a := setup(t)
	d.Cfg.Fleet.Serve = true
	a.obs[0].FilesTouched = []model.FileTouch{{Path: "/r/file", TS: d.Now()}}
	pass(t, d, st, Plan{Procs: true, Machine: true})
	first := feedRows(t, st, 0)
	if len(first) == 0 {
		t.Fatal("no feed")
	}
	counts := map[string]int{}
	for _, row := range first {
		counts[row.Kind]++
	}
	if counts["session"] != 1 || counts["session_files"] != 1 || counts["machine"] != 1 {
		t.Fatalf("first counts: %v", counts)
	}
	pass(t, d, st, Plan{Procs: true, Machine: true})
	for _, row := range feedRows(t, st, first[len(first)-1].Seq) {
		if row.Kind == "session" || row.Kind == "session_files" || row.Kind == "machine" {
			t.Fatalf("unchanged row: %+v", row)
		}
	}
	a.obs[0].Title = "changed"
	pass(t, d, st, Plan{})
	rows := feedRows(t, st, first[len(first)-1].Seq)
	found := false
	for _, row := range rows {
		if row.Kind == "session" && strings.Contains(string(row.Payload), "changed") {
			found = true
		}
	}
	if !found {
		t.Fatal("changed session absent")
	}
}

func TestMixerInputAgesDoNotRepublishSession(t *testing.T) {
	d, st, _, _ := setup(t)
	d.Cfg.Fleet.Serve = true
	pass(t, d, st, Plan{Procs: true})
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		return tx.InsertEvent(model.Event{Adapter: "claude", ExternalID: "one", Kind: "otlp:api_error", TS: d.Now(), Summary: "API unavailable"})
	}); err != nil {
		t.Fatal(err)
	}
	pass(t, d, st, Plan{})
	first := feedRows(t, st, 0)
	at := d.Now().Add(time.Second)
	d.Now = func() time.Time { return at }
	pass(t, d, st, Plan{})
	for _, row := range feedRows(t, st, first[len(first)-1].Seq) {
		if row.Kind == "session" {
			t.Fatal("retained input age alone republished session")
		}
	}
}

func TestFeedSeqMonotonicAcrossRestart(t *testing.T) {
	_, _, _, _ = setup(t)
	path := filepath.Join(t.TempDir(), "driga.db")
	st, err := store.Open(path, store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	appendRow := func(st *store.Store) int64 {
		t.Helper()
		var seq int64
		if err := st.Tx(context.Background(), func(tx *store.Tx) (err error) {
			seq, err = tx.AppendFeed("source", "x", []byte(`{}`), time.Now())
			return
		}); err != nil {
			t.Fatal(err)
		}
		return seq
	}
	one := appendRow(st)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = store.Open(path, store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if two := appendRow(st); two <= one {
		t.Fatalf("seq %d -> %d", one, two)
	}
}

func TestEpochCreatedOnce(t *testing.T) {
	_, _, _, _ = setup(t)
	path := filepath.Join(t.TempDir(), "driga.db")
	st, err := store.Open(path, store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	one, _, _, err := st.FeedBounds()
	if err != nil || len(one) != 32 {
		t.Fatalf("epoch %q %v", one, err)
	}
	st.Close()
	st, err = store.Open(path, store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	two, _, _, err := st.FeedBounds()
	if err != nil || one != two {
		t.Fatalf("epoch %q -> %q: %v", one, two, err)
	}
}

func TestPruneEmitsDeletes(t *testing.T) {
	d, st, _, _ := setup(t)
	old := d.Now().Add(-31 * 24 * time.Hour)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		id, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "codex", ExternalID: "old"}, EndedAt: &old})
		if err != nil {
			return err
		}
		if err := tx.InsertEvent(model.Event{Adapter: "codex", ExternalID: "old", TS: old, Kind: "Stop"}); err != nil {
			return err
		}
		if err := tx.UpsertInboxItem(model.InboxItem{ID: "old", SessionID: id, TS: old}); err != nil {
			return err
		}
		return tx.Prune(d.Now())
	}); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, row := range feedRows(t, st, 0) {
		if row.Kind == "delete" {
			var v feed.DeleteRow
			if err := json.Unmarshal(row.Payload, &v); err != nil {
				t.Fatal(err)
			}
			got[string(v.Kind)] = true
		}
	}
	for _, kind := range []string{"session", "event", "inbox"} {
		if !got[kind] {
			t.Fatalf("missing %s delete: %v", kind, got)
		}
	}
}

func TestRemoteURLCredentialsStripped(t *testing.T) {
	d, st, _, _ := setup(t)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		_, err := tx.UpsertRepo(model.Repo{Path: "/r", RemoteURL: "https://u:p@host/x"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	pass(t, d, st, Plan{})
	repos, err := st.Repos()
	if err != nil || repos[0].RemoteURL != "https://host/x" {
		t.Fatalf("local repos: %+v %v", repos, err)
	}
	_, b := feed.Encode(feed.Repo, model.Repo{Path: "/r", RemoteURL: "https://u:p@host/x"}, feed.Titles)
	if strings.Contains(string(b), "u:p") || !strings.Contains(string(b), "https://host/x") {
		t.Fatalf("payload: %s", b)
	}
}

func TestFeedCursorRetentionAndRollback(t *testing.T) {
	d, st, _, _ := setup(t)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		_, err := tx.AppendFeed("source", "old", []byte(`{}`), d.Now().Add(-31*24*time.Hour))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Tx(context.Background(), func(tx *store.Tx) error { return tx.Prune(d.Now()) }); err != nil {
		t.Fatal(err)
	}
	epoch, min, max, err := st.FeedBounds()
	if err != nil || min != max+1 {
		t.Fatalf("empty bounds %d %d: %v", min, max, err)
	}
	if _, err := st.FeedSince(epoch, 0, 0); !errors.Is(err, store.ErrResnapshot) {
		t.Fatalf("old cursor: %v", err)
	}
	if _, err := st.FeedSince("wrong", max, 0); !errors.Is(err, store.ErrResnapshot) {
		t.Fatalf("epoch: %v", err)
	}
	rolledBack := errors.New("rollback")
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		if _, err := tx.AppendFeed("source", "x", []byte(`{}`), d.Now()); err != nil {
			return err
		}
		return rolledBack
	}); !errors.Is(err, rolledBack) {
		t.Fatal(err)
	}
	_, _, after, _ := st.FeedBounds()
	if after != max {
		t.Fatal("feed survived rollback")
	}
}

func TestFeedAckAndOrphanDisappearance(t *testing.T) {
	d, st, p, a := setup(t)
	d.Cfg.Fleet.Serve = true
	if err := st.SetMeta("baseline_at", d.Now().Add(-time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	a.obs[0].Completions = []model.CompletionEvent{{ID: "done", TS: d.Now()}}
	p.procs = append(p.procs, model.Proc{PID: 200, PPID: 1, StartTime: d.Now().Add(-time.Hour), Cmdline: "claude mcp serve"})
	pass(t, d, st, Plan{Procs: true})
	first := feedRows(t, st, 0)
	seq := first[len(first)-1].Seq
	if err := spool.Append(d.SpoolDir, model.HookEvent{TS: d.Now(), Event: "ack", Message: inbox(t, st)[0].ID}); err != nil {
		t.Fatal(err)
	}
	p.procs = p.procs[:1]
	pass(t, d, st, Plan{Procs: true})
	ack, deleted := false, false
	for _, row := range feedRows(t, st, seq) {
		if row.Kind == "inbox" && strings.Contains(string(row.Payload), "acked_at") {
			ack = true
		}
		if row.Kind == "delete" && strings.Contains(string(row.Payload), "orphan") {
			deleted = true
		}
	}
	if !ack || !deleted {
		t.Fatalf("ack=%v deleted=%v", ack, deleted)
	}
}

func TestFeedRetentionKeepsResumablePrefix(t *testing.T) {
	d, st, _, _ := setup(t)
	var seqs []int64
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		for _, ts := range []time.Time{d.Now(), d.Now().Add(-31 * 24 * time.Hour), d.Now()} {
			seq, err := tx.AppendFeed("source", "x", []byte(`{}`), ts)
			if err != nil {
				return err
			}
			seqs = append(seqs, seq)
		}
		return tx.Prune(d.Now())
	}); err != nil {
		t.Fatal(err)
	}
	epoch, _, _, err := st.FeedBounds()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.FeedSince(epoch, seqs[0], 1); !errors.Is(err, store.ErrResnapshot) {
		t.Fatalf("cursor crossed pruned hole: %v", err)
	}
	rows, err := st.FeedSince(epoch, seqs[1], 1)
	if err != nil || len(rows) != 1 || rows[0].Seq != seqs[2] {
		t.Fatalf("valid resume: %+v %v", rows, err)
	}
}

func TestPassPublishesChangedGatedSession(t *testing.T) {
	d, st, _, a := setup(t)
	d.Cfg.Fleet.Serve = true
	d.Cfg.Fleet.ShareText = "none"
	pass(t, d, st, Plan{Procs: true})
	_, _, seq, err := st.FeedBounds()
	if err != nil {
		t.Fatal(err)
	}
	a.obs[0].Title = "withheld title"
	pass(t, d, st, Plan{})
	found := false
	for _, row := range feedRows(t, st, seq) {
		if row.Kind == "session" {
			found = true
			if strings.Contains(string(row.Payload), "withheld title") {
				t.Fatal("title leaked")
			}
		}
	}
	if !found {
		t.Fatal("stored session changed without a feed row")
	}
}

func TestUnchangedWorktreeNotReappended(t *testing.T) {
	d, st, _, a := setup(t)
	d.Cfg.Fleet.Serve = true
	repo := t.TempDir()
	if out, err := exec.Command("git", "init", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %s: %v", out, err)
	}
	a.obs[0].Cwd = repo
	pass(t, d, st, Plan{Git: true, GitFull: true})
	first := feedRows(t, st, 0)
	counts := func(rows []store.FeedRow, kind string) int {
		n := 0
		for _, row := range rows {
			if row.Kind == kind {
				n++
			}
		}
		return n
	}
	if counts(first, "worktree") != 1 {
		t.Fatalf("initial worktrees: %v", first)
	}
	seq := first[len(first)-1].Seq
	before, err := st.Worktrees(0)
	if err != nil || len(before) != 1 {
		t.Fatalf("initial worktrees: %v %v", before, err)
	}
	now := d.Now().Add(time.Second) // Less than every source's collection interval.
	d.Now = func() time.Time { return now }
	pass(t, d, st, Plan{Git: true, GitFull: true})
	rows := feedRows(t, st, seq)
	if counts(rows, "worktree") != 0 {
		t.Error("unchanged worktree re-appended")
	}
	if counts(rows, "source") != 0 {
		t.Error("source freshness was published before five minutes")
	}
	ws, err := st.Worktrees(0)
	if err != nil || len(ws) != 1 || !ws[0].UpdatedAt.After(before[0].UpdatedAt) {
		t.Fatalf("bookkeeping not refreshed: %+v %v", ws, err)
	}
	if len(rows) > 0 {
		seq = rows[len(rows)-1].Seq
	}
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repo, "add", "new.txt").CombinedOutput(); err != nil {
		t.Fatalf("git add: %s: %v", out, err)
	}
	now = now.Add(time.Second)
	pass(t, d, st, Plan{Git: true, GitFull: true})
	if counts(feedRows(t, st, seq), "worktree") != 1 {
		t.Fatal("real worktree change not appended")
	}
}
