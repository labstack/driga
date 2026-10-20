package derive

import (
	"reflect"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func inboxSession(id int64, adapter, external string, ts time.Time) model.Session {
	return model.Session{ID: id, SessionObs: model.SessionObs{Adapter: adapter, ExternalID: external, Completions: []model.CompletionEvent{{ID: "turn:1", TS: ts, Text: "done"}}}}
}

func TestInboxIDsStableAndDeduped(t *testing.T) {
	ts := time.Unix(100, 123).UTC()
	s := inboxSession(1, "codex", "thread", ts)
	s.Completions = append(s.Completions, s.Completions[0])
	c := inboxSession(2, "claude", "session", ts)
	c.Completions = nil
	hooks := []model.HookEvent{{TS: ts, Adapter: "claude", SessionID: "session", Event: "Stop", Message: "hook done"}, {TS: ts, Adapter: "codex", SessionID: "thread", Event: "notify", Message: "duplicate completion"}, {TS: ts, Adapter: "claude", SessionID: "unmatched", Event: "Notification"}, {TS: ts, Adapter: "other", SessionID: "session", Event: "Stop"}}
	hooks = append(hooks, hooks[0])
	sessions := []model.Session{s, c}
	got := InboxItems(sessions, nil, hooks, time.Time{})
	if len(got) != 2 {
		t.Fatal(got)
	}
	ids := map[string]bool{}
	for _, item := range got {
		ids[item.ID] = true
		if item.Kind != "finished" || item.Summary == "" {
			t.Fatal(item)
		}
	}
	if !ids["codex:thread:turn:1"] || !ids["claude:session:100000000123"] {
		t.Fatal(ids)
	}
	if again := InboxItems(sessions, nil, hooks, time.Time{}); !reflect.DeepEqual(again, got) {
		t.Fatal(again, got)
	}
	if reverse := InboxItems([]model.Session{c, s}, nil, hooks, time.Time{}); !reflect.DeepEqual(reverse, got) {
		t.Fatal(reverse, got)
	}
}

func TestInboxBaseline(t *testing.T) {
	baseline := time.Unix(100, 0).UTC()
	s := inboxSession(1, "claude", "session", baseline)
	s.Completions = append(s.Completions, model.CompletionEvent{ID: "old", TS: baseline.Add(-time.Second)}, model.CompletionEvent{ID: "new", TS: baseline.Add(time.Second)})
	hooks := []model.HookEvent{{TS: baseline, Adapter: "claude", SessionID: "session", Event: "Notification"}}
	got := InboxItems([]model.Session{s}, nil, hooks, baseline)
	if len(got) != 4 {
		t.Fatal(got)
	}
	for _, item := range got {
		if item.Kind == "waiting" {
			if item.AckedAt != nil {
				t.Fatal(item)
			}
			continue
		}
		if item.TS.After(baseline) {
			if item.AckedAt != nil {
				t.Fatal(item)
			}
		} else if item.AckedAt == nil || !item.AckedAt.Equal(baseline) {
			t.Fatal(item)
		}
		if item.Summary == "" {
			t.Fatal(item)
		}
	}
}

func TestChildFinishedCounted(t *testing.T) {
	ts := time.Unix(100, 0).UTC()
	sessions := []model.Session{inboxSession(1, "claude", "parent", ts), inboxSession(2, "codex", "child", ts), inboxSession(3, "claude", "grandchild", ts)}
	sessions[0].Completions = nil
	sessions[1].Completions = append(sessions[1].Completions, sessions[1].Completions[0], model.CompletionEvent{ID: "old", TS: ts.Add(-time.Hour)})
	parents := map[int64]int64{2: 1, 3: 2}
	hooks := []model.HookEvent{{TS: ts, Adapter: "claude", SessionID: "grandchild", Event: "Stop"}, {TS: ts, Adapter: "codex", SessionID: "child", Event: "notify"}}
	got := InboxItems(sessions, parents, hooks, ts.Add(-time.Minute))
	if len(got) != 4 {
		t.Fatal(got)
	}
	for _, item := range got {
		if item.Kind != "worker_finished" || item.SessionID != 1 {
			t.Fatal(item)
		}
	}
	if got := FinishedChildCounts(sessions, parents, hooks, ts.Add(-time.Minute)); !reflect.DeepEqual(got, map[int64]int{1: 3}) {
		t.Fatal(got)
	}
}

func TestChildWaitingStaysOnWorker(t *testing.T) {
	ts := time.Unix(100, 0).UTC()
	sessions := []model.Session{inboxSession(1, "claude", "parent", ts), inboxSession(2, "claude", "child", ts), inboxSession(3, "claude", "grandchild", ts)}
	for i := range sessions {
		sessions[i].Completions = nil
	}
	hooks := []model.HookEvent{{TS: ts, Adapter: "claude", SessionID: "grandchild", Event: "Notification", Message: "approval needed"}}
	got := InboxItems(sessions, map[int64]int64{2: 1, 3: 2}, hooks, time.Time{})
	if len(got) != 1 || got[0].SessionID != 3 || got[0].Kind != "waiting" || got[0].Summary != "approval needed" || got[0].ID != "claude:grandchild:100000000000" {
		t.Fatal(got)
	}
}

// Adapters already namespace completion IDs ("codex:<sid>:<offset>"); the
// inbox ID must not repeat that prefix.
func TestInboxIDNotDoublePrefixed(t *testing.T) {
	ts := time.Unix(100, 0).UTC()
	s := inboxSession(1, "codex", "sid", ts)
	s.Completions = []model.CompletionEvent{{ID: "codex:sid:932548", TS: ts}}
	got := InboxItems([]model.Session{s}, nil, nil, time.Time{})
	if len(got) != 1 || got[0].ID != "codex:sid:932548" {
		t.Fatal(got)
	}
}
