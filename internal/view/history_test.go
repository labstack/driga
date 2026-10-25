package view

import (
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

// History lists top-level sessions that ended within the window, newest
// first, with a resume command for agents that have one. Live sessions,
// workers and sessions that ended too long ago stay out.
func TestBoardHistoryListsEndedSessions(t *testing.T) {
	s := fixture(t, func(tx *store.Tx) error {
		at := func(d time.Duration) *time.Time { t := testNow.Add(-d); return &t }
		parent, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "recent", Cwd: "/repo", Title: "fix login"}, Label: "finished", EndedAt: at(time.Hour)})
		if err != nil {
			return err
		}
		for _, x := range []model.Session{
			{SessionObs: model.SessionObs{Adapter: "codex", ExternalID: "older", Cwd: "/repo", Title: "add tests"}, Label: "finished", EndedAt: at(3 * 24 * time.Hour)},
			{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "stale", Cwd: "/repo"}, Label: "finished", EndedAt: at(HistoryWindow + time.Hour)},
			{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "worker", Cwd: "/repo"}, ParentID: &parent, Label: "finished", EndedAt: at(time.Hour)},
			{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "live", Cwd: "/repo"}, Label: "working"},
			{SessionObs: model.SessionObs{Adapter: "agy", ExternalID: "noresume", Cwd: "/repo", Title: "explore"}, Label: "finished", EndedAt: at(2 * time.Hour)},
		} {
			if _, err := tx.UpsertSession(x); err != nil {
				return err
			}
		}
		return nil
	})
	var b BoardSnapshot
	if err := s.Tx(t.Context(), func(tx *store.Tx) error {
		var err error
		b, err = LoadBoardSnapshot(tx, Filter{}, testNow)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, h := range b.History {
		titles = append(titles, h.Title)
	}
	if len(b.History) != 3 || titles[0] != "fix login" || titles[1] != "explore" || titles[2] != "add tests" {
		t.Fatalf("history = %q", titles)
	}
	if got := b.History[0].Resume; got != "cd '/repo' && claude --resume 'recent'" {
		t.Fatalf("claude resume = %q", got)
	}
	if got := b.History[2].Resume; got != "cd '/repo' && codex resume 'older'" {
		t.Fatalf("codex resume = %q", got)
	}
	if b.History[1].Resume != "" {
		t.Fatalf("an agent without resume has no command: %q", b.History[1].Resume)
	}
}
