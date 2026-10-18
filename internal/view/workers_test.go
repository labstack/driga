package view_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/tui"
	"github.com/labstack/driga/internal/view"
)

var workerNow = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

func workerStore(t *testing.T) (*store.Store, int64) {
	t.Helper()
	s, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	var parent int64
	err = s.Tx(context.Background(), func(tx *store.Tx) error {
		if err := tx.UpsertMachine(model.Machine{Hostname: "test-host"}); err != nil {
			return err
		}
		baseline := workerNow.Add(-time.Hour)
		ended := workerNow.Add(-time.Minute)
		ss := []model.Session{
			{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "parent", StartedAt: baseline, LastActivityAt: workerNow, Completions: []model.CompletionEvent{{ID: "parent-turn", TS: workerNow}}}, Label: "working"},
			{SessionObs: model.SessionObs{Adapter: "codex", ExternalID: "child", StartedAt: baseline, LastActivityAt: workerNow, Completions: []model.CompletionEvent{{ID: "child-turn", TS: workerNow}}}, Label: "finished", EndedAt: &ended},
			{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "grandchild", StartedAt: baseline, LastActivityAt: workerNow, Completions: []model.CompletionEvent{{ID: "grandchild-turn", TS: workerNow}, {ID: "old", TS: baseline}}}, Label: "finished", EndedAt: &ended},
		}
		parents := map[int64]int64{}
		for i := range ss {
			if i > 0 {
				ss[i].ParentID = &ss[i-1].ID
			}
			ss[i].ID, err = tx.UpsertSession(ss[i])
			if err != nil {
				return err
			}
			if i > 0 {
				parents[ss[i].ID] = ss[i-1].ID
			}
		}
		parent = ss[0].ID
		ss[1].Completions = append(ss[1].Completions, ss[1].Completions[0])
		hooks := []model.HookEvent{
			{Adapter: "claude", SessionID: "grandchild", Event: "Stop", TS: workerNow.Add(-time.Second)},
			{Adapter: "claude", SessionID: "grandchild", Event: "Notification", TS: workerNow},
		}
		// Match the collector's derive-to-store path, including replay.
		for range 2 {
			for _, item := range derive.InboxItems(ss, parents, hooks, baseline) {
				if err := tx.UpsertInboxItem(item); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, parent
}

func TestChildFinishedStoredAsWorkerItem(t *testing.T) {
	s, parent := workerStore(t)
	items, err := s.Inbox(time.Time{}, true)
	if err != nil {
		t.Fatal(err)
	}
	workers, unseen := 0, 0
	for _, item := range items {
		if item.Kind == "worker-finished" {
			workers++
			if item.SessionID != parent {
				t.Fatalf("worker completion must retain its top-level parent: %+v", item)
			}
			if item.AckedAt == nil {
				unseen++
			}
		} else if item.SessionID != parent {
			t.Fatalf("attention or parent completion must attach to root: %+v", item)
		}
	}
	if workers != 4 || unseen != 3 || len(items) != 6 {
		t.Fatalf("stored %d items, %d worker completions (%d unseen); want 6, 4, 3: %+v", len(items), workers, unseen, items)
	}
}

func TestAgentsShowsWorkerFinishedCount(t *testing.T) {
	s, parent := workerStore(t)
	check := func(want int) {
		t.Helper()
		v, err := view.Agents(s, view.AgentFilter{}, workerNow)
		if err != nil {
			t.Fatal(err)
		}
		if len(v.Rows) != 1 || v.Rows[0].ID != parent || v.Rows[0].FinishedChildren != want || len(v.Rows[0].Children) != 0 {
			t.Fatalf("ended workers must count on live root: %+v, want %d", v.Rows, want)
		}
		var out bytes.Buffer
		if err := view.Render(&out, v, view.RenderOpts{}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), fmt.Sprintf("(%d workers finished)", want)) {
			t.Fatalf("missing worker count: %s", out.String())
		}
	}
	check(3)
	if err := s.Tx(context.Background(), func(tx *store.Tx) error {
		_, err := tx.AckInbox("codex:child:child-turn", workerNow)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	check(2)
}

func TestAgentsCountsWorkersAfterBrokerExits(t *testing.T) {
	s, parent := workerStore(t)
	// The collector recomputes family links from live processes on each pass.
	// Once a broker exits, the completion must still count on its original root.
	if err := s.Tx(context.Background(), func(tx *store.Tx) error {
		ss, err := tx.Sessions(store.SessionFilter{IncludeEnded: true})
		if err != nil {
			return err
		}
		for _, session := range ss {
			session.ParentID = nil
			if _, err := tx.UpsertSession(session); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	v, err := view.Agents(s, view.AgentFilter{}, workerNow)
	if err != nil || len(v.Rows) != 1 || v.Rows[0].ID != parent || v.Rows[0].FinishedChildren != 3 {
		t.Fatalf("completion counts must survive broker exit: %+v, %v", v, err)
	}
}

func TestWorkerItemsNotListedInInbox(t *testing.T) {
	s, parent := workerStore(t)
	// Seed explicit worker records so this reproduces the listing bug even
	// before derivation learns to retain worker completions.
	if err := s.Tx(context.Background(), func(tx *store.Tx) error {
		for _, ack := range []*time.Time{nil, &workerNow} {
			id := "worker-unacked"
			if ack != nil {
				id = "worker-acked"
			}
			if err := tx.UpsertInboxItem(model.InboxItem{ID: id, SessionID: parent, Kind: "worker-finished", TS: workerNow, AckedAt: ack}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, all := range []bool{false, true} {
		v, err := view.Inbox(s, view.Filter{All: all}, workerNow)
		if err != nil {
			t.Fatal(err)
		}
		if len(v.Rows) != 2 {
			t.Fatalf("all=%v: want only parent completion and worker attention: %+v", all, v.Rows)
		}
		for _, row := range v.Rows {
			if row.Kind == "worker-finished" {
				t.Fatalf("worker item listed: %+v", row)
			}
		}
	}
	summary, err := view.Summary(s, view.Filter{}, workerNow)
	if err != nil || summary.Inbox != 2 {
		t.Fatalf("summary inbox=%d, want 2: %v", summary.Inbox, err)
	}
	var out bytes.Buffer
	if err := view.Render(&out, summary, view.RenderOpts{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "· inbox 2 ·") {
		t.Fatalf("summary line counts workers: %s", out.String())
	}
	source := tui.NewStoreSource(s, t.TempDir(), view.Filter{})
	source.Now = func() time.Time { return workerNow }
	snap, err := source.Load(context.Background())
	if err != nil || snap.Summary.Inbox != 2 || len(snap.Inbox.Rows) != 2 {
		t.Fatalf("TUI inbox counts workers: summary=%d, rows=%d, err=%v", snap.Summary.Inbox, len(snap.Inbox.Rows), err)
	}
}
