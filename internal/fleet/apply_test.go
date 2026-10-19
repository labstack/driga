package fleet

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/labstack/driga/internal/feed"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func remoteStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}
func raw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func remoteSnapshot(t *testing.T, at time.Time, epoch string) *Snapshot {
	return &Snapshot{Schema: 1, ObservedAt: at, Data: SnapshotData{Machine: raw(t, model.Machine{MachineID: "remote", Hostname: "vm"}), Cursor: Cursor{epoch, 0}, Sessions: []json.RawMessage{raw(t, feed.SessionRow{Session: model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "one", LastActivityAt: at.Add(-time.Second)}, Label: "working"}})}}}
}
func apply(t *testing.T, st *store.Store, b Batch, now time.Time) {
	t.Helper()
	if err := st.Tx(context.Background(), func(tx *store.Tx) error { return ApplyBatch(tx, b, now) }); err != nil {
		t.Fatal(err)
	}
}
func TestApplyClampsFutureTimestamps(t *testing.T) {
	st := remoteStore(t)
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	apply(t, st, Batch{MachineID: "remote", Snapshot: remoteSnapshot(t, now.Add(5*time.Minute), "epoch")}, now)
	ss, e := st.SessionsScoped(store.SessionFilter{IncludeEnded: true}, store.Scope{})
	if e != nil || len(ss) != 1 {
		t.Fatalf("%+v %v", ss, e)
	}
	if ss[0].LastActivityAt.After(now) || now.Sub(ss[0].LastActivityAt) != time.Second || ss[0].Label != "working" {
		t.Fatalf("peer ages/label lost: %+v", ss[0])
	}
	ms, _ := st.Machines()
	if len(ms) != 1 || ms[0].ClockSkewMS != 300000 {
		t.Fatalf("skew: %+v", ms)
	}
}
func TestPeerStopMarksStale(t *testing.T) {
	st := remoteStore(t)
	now := time.Now().UTC()
	apply(t, st, Batch{MachineID: "remote", Snapshot: remoteSnapshot(t, now, "epoch")}, now)
	for _, d := range []time.Duration{44 * time.Second, 45 * time.Second} {
		if err := st.Tx(context.Background(), func(tx *store.Tx) error { return RefreshStates(tx, now.Add(d)) }); err != nil {
			t.Fatal(err)
		}
		ms, _ := st.Machines()
		want := "live"
		if d == 45*time.Second {
			want = "stale"
		}
		if ms[0].State != want {
			t.Fatalf("at %v: %s", d, ms[0].State)
		}
	}
}
func TestIncompatibleSchemaAppliesNothing(t *testing.T) {
	st := remoteStore(t)
	now := time.Now()
	snap := remoteSnapshot(t, now, "epoch")
	snap.Schema = 2
	if e := st.Tx(context.Background(), func(tx *store.Tx) error { return ApplyBatch(tx, Batch{MachineID: "remote", Snapshot: snap}, now) }); e == nil {
		t.Fatal("accepted schema 2")
	}
	ss, _ := st.SessionsScoped(store.SessionFilter{IncludeEnded: true}, store.Scope{})
	if len(ss) != 0 {
		t.Fatal(ss)
	}
}
func TestRemoteRetention(t *testing.T) {
	st := remoteStore(t)
	now := time.Now().UTC()
	old := now.Add(-40 * 24 * time.Hour)
	snap := remoteSnapshot(t, now, "epoch")
	snap.Data.Sessions = []json.RawMessage{raw(t, feed.SessionRow{Session: model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "old", LastActivityAt: old}, EndedAt: &old}})}
	snap.Data.Procs = []json.RawMessage{raw(t, model.Proc{PID: 10, Agent: "claude"})}
	snap.Data.MachineSample = raw(t, model.MachineSample{TS: old})
	apply(t, st, Batch{MachineID: "remote", Snapshot: snap}, now)
	if e := st.Tx(context.Background(), func(tx *store.Tx) error { return tx.Prune(now.Add(31 * 24 * time.Hour)) }); e != nil {
		t.Fatal(e)
	}
	ss, _ := st.SessionsScoped(store.SessionFilter{IncludeEnded: true}, store.Scope{})
	ps, _ := st.ProcsScoped(store.Scope{})
	_, sampleErr := st.LatestSampleFor("remote")
	if len(ss) != 0 || len(ps) != 0 || sampleErr == nil {
		t.Fatalf("retained sessions=%v procs=%v sample=%v", ss, ps, sampleErr)
	}
	if e := st.Tx(context.Background(), func(tx *store.Tx) error { return tx.PurgeRemote("remote") }); e != nil {
		t.Fatal(e)
	}
	ms, _ := st.Machines()
	if len(ms) != 0 {
		t.Fatal(ms)
	}
}
func TestApplyNaturalReferencesAndAtomicCursor(t *testing.T) {
	st := remoteStore(t)
	now := time.Now().UTC()
	snap := remoteSnapshot(t, now, "epoch")
	snap.Data.Repos = []json.RawMessage{raw(t, model.Repo{ID: 999, Path: "/r"})}
	snap.Data.Worktrees = []json.RawMessage{raw(t, feed.WorktreeRow{Worktree: model.Worktree{ID: 888, RepoID: 999, Path: "/r"}, RepoPath: "/r"})}
	snap.Data.Sessions = []json.RawMessage{raw(t, feed.SessionRow{Session: model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "child"}}, Parent: &feed.SessionKey{Adapter: "claude", ExternalID: "parent"}, RepoPath: "/r", WorktreePath: "/r"}), raw(t, feed.SessionRow{Session: model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "parent"}}})}
	snap.Data.InboxItems = []json.RawMessage{raw(t, feed.InboxRow{InboxItem: model.InboxItem{ID: "item", TS: now}, Session: feed.SessionKey{Adapter: "claude", ExternalID: "child"}})}
	apply(t, st, Batch{MachineID: "remote", Snapshot: snap}, now)
	ss, _ := st.SessionsScoped(store.SessionFilter{IncludeEnded: true}, store.Scope{})
	var child, parent model.Session
	for _, s := range ss {
		if s.ExternalID == "child" {
			child = s
		} else {
			parent = s
		}
	}
	if child.ParentID == nil || *child.ParentID != parent.ID || child.RepoID == nil || *child.RepoID == 999 || child.WorktreeID == nil {
		t.Fatalf("bad references: %+v", ss)
	}
	key, p := feed.Encode(feed.Event, model.Event{Adapter: "claude", ExternalID: "child", TS: now, Kind: "Stop"}, feed.Titles)
	b := Batch{MachineID: "remote", Rows: []FeedRow{{Seq: 1, Kind: string(feed.Event), Key: key, Payload: p}}, Cursor: Cursor{"epoch", 1}}
	apply(t, st, b, now)
	apply(t, st, b, now)
	e := st.Tx(context.Background(), func(tx *store.Tx) error {
		es, _ := tx.ForMachine("remote").LatestEvents(time.Time{})
		if len(es) != 1 {
			t.Fatalf("duplicate event: %v", es)
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	b.Rows = []FeedRow{{Seq: 3, Kind: "session", Payload: json.RawMessage(`{}`)}}
	b.Cursor.Seq = 3
	if e := st.Tx(context.Background(), func(tx *store.Tx) error { return ApplyBatch(tx, b, now) }); e == nil {
		t.Fatal("accepted missing sequence")
	}
	ms, _ := st.Machines()
	if ms[0].FeedSeq != 1 {
		t.Fatal(ms)
	}
	ack := now.Add(time.Second)
	key, p = feed.Encode(feed.Inbox, feed.InboxRow{InboxItem: model.InboxItem{ID: "item", TS: now, AckedAt: &ack}, Session: feed.SessionKey{Adapter: "claude", ExternalID: "child"}}, feed.Titles)
	apply(t, st, Batch{MachineID: "remote", Rows: []FeedRow{{Seq: 2, Kind: string(feed.Inbox), Key: key, Payload: p}}, Cursor: Cursor{"epoch", 2}}, ack)
	items, _ := st.InboxScoped(time.Time{}, true, store.Scope{})
	if len(items) != 1 || items[0].AckedAt == nil {
		t.Fatal(items)
	}
}

func TestRemoteRetentionInvalidatesCursor(t *testing.T) {
	st := remoteStore(t)
	now := time.Now().UTC()
	apply(t, st, Batch{MachineID: "remote", Snapshot: remoteSnapshot(t, now, "epoch")}, now)
	later := now.Add(31 * 24 * time.Hour)
	if e := st.Tx(context.Background(), func(tx *store.Tx) error { return tx.PruneRemote(later) }); e != nil {
		t.Fatal(e)
	}
	if e := st.Tx(context.Background(), func(tx *store.Tx) error {
		return ApplyBatch(tx, Batch{MachineID: "remote", Cursor: Cursor{"epoch", 0}, ContactAt: later}, later)
	}); e != store.ErrResnapshot {
		t.Fatalf("resumed an expired cache: %v", e)
	}
}
func TestRemoteReplacementAndDeletes(t *testing.T) {
	st := remoteStore(t)
	now := time.Now().UTC()
	snap := remoteSnapshot(t, now.Add(5*time.Minute), "epoch")
	snap.Data.Procs = []json.RawMessage{raw(t, feed.ProcRow{Proc: model.Proc{PID: 1, Agent: "claude", SessionID: 999}, Session: &feed.SessionKey{Adapter: "claude", ExternalID: "one"}})}
	event := model.Event{Adapter: "claude", ExternalID: "one", Kind: "Stop", TS: snap.ObservedAt.Add(-time.Second)}
	_, payload := feed.Encode(feed.Event, event, feed.Titles)
	snap.Data.Events = []json.RawMessage{payload}
	apply(t, st, Batch{MachineID: "remote", Snapshot: snap}, now)
	key, _ := feed.Encode(feed.Event, event, feed.Titles)
	_, payload = feed.Encode(feed.Delete, feed.DeleteRow{Kind: feed.Event, Key: key}, feed.None)
	apply(t, st, Batch{MachineID: "remote", Rows: []FeedRow{{Seq: 1, Kind: "delete", Payload: payload}}, Cursor: Cursor{"epoch", 1}}, now)
	if e := st.Tx(context.Background(), func(tx *store.Tx) error {
		es, e := tx.ForMachine("remote").LatestEvents(time.Time{})
		if e != nil || len(es) != 0 {
			t.Fatalf("deleted wrong timestamp: %v %v", es, e)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	snap = remoteSnapshot(t, now, "new-epoch")
	snap.Data.Sessions = nil
	apply(t, st, Batch{MachineID: "remote", Snapshot: snap}, now)
	ss, _ := st.SessionsScoped(store.SessionFilter{IncludeEnded: true}, store.Scope{})
	ps, _ := st.ProcsScoped(store.Scope{})
	if len(ss) != 0 || len(ps) != 0 {
		t.Fatalf("snapshot unioned old rows: %v %v", ss, ps)
	}
}

func TestPeerLossRecordsOneCoverageGap(t *testing.T) {
	st := remoteStore(t)
	now := time.Now().UTC()
	apply(t, st, Batch{MachineID: "remote", Snapshot: remoteSnapshot(t, now, "epoch")}, now)
	for _, seconds := range []int{1, 4, 10} {
		apply(t, st, Batch{MachineID: "remote", Disconnected: true, Error: "connection lost"}, now.Add(time.Duration(seconds)*time.Second))
	}
	apply(t, st, Batch{MachineID: "remote", Cursor: Cursor{"epoch", 0}, ContactAt: now.Add(20 * time.Second)}, now.Add(20*time.Second))
	if e := st.Tx(context.Background(), func(tx *store.Tx) error {
		g, e := tx.ForMachine("remote").Gaps(time.Time{})
		if e != nil || len(g) != 1 || !g[0].From.Equal(now) || !g[0].To.Equal(now.Add(20*time.Second)) {
			t.Fatalf("coverage gaps: %+v %v", g, e)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}

func TestApplyRejectsMissingEpochAndRollsBack(t *testing.T) {
	st := remoteStore(t)
	now := time.Now().UTC()
	apply(t, st, Batch{MachineID: "remote", Snapshot: remoteSnapshot(t, now, "epoch")}, now)
	key, p := feed.Encode(feed.Repo, model.Repo{Path: "/never-committed"}, feed.Titles)
	for _, b := range []Batch{
		{MachineID: "remote", Rows: []FeedRow{{Seq: 1, Kind: "repo", Key: key, Payload: p}}},
		{MachineID: "remote", Rows: []FeedRow{{Seq: 1, Kind: "repo", Key: key, Payload: p}, {Seq: 2, Kind: "invalid", Payload: json.RawMessage(`{}`)}}, Cursor: Cursor{"epoch", 2}},
		{MachineID: "remote", Rows: []FeedRow{{Seq: 1, Kind: "repo", Key: key, Payload: p}}, Cursor: Cursor{"epoch", 0}},
	} {
		if e := st.Tx(context.Background(), func(tx *store.Tx) error { return ApplyBatch(tx, b, now) }); e == nil {
			t.Fatal("accepted invalid batch")
		}
		rs, _ := st.ReposScoped(store.Scope{})
		ms, _ := st.Machines()
		if len(rs) != 0 || ms[0].FeedSeq != 0 {
			t.Fatalf("partial apply: %v %v", rs, ms)
		}
	}
}
