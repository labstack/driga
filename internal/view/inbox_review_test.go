package view

import (
	"slices"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

// A finished turn carries what there is to review: the files its session
// edited and its checkout's uncommitted, unpushed and latest commit. A wait
// carries neither.
func TestInboxFinishedCarriesReview(t *testing.T) {
	s := fixture(t, func(tx *store.Tx) error {
		repo, err := tx.UpsertRepo(model.Repo{Path: "/repo", Name: "repo"})
		if err != nil {
			return err
		}
		wt, err := tx.UpsertWorktree(model.Worktree{RepoID: repo, Path: "/repo/wt", Branch: "feat", Dirty: 2, Untracked: 1, Ahead: 3, LastCommitSubject: "add review"})
		if err != nil {
			return err
		}
		for _, id := range []string{"done", "waiting"} {
			sid, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: id, Cwd: "/repo/wt", Branch: "feat"}, RepoID: &repo, WorktreeID: &wt, Label: id})
			if err != nil {
				return err
			}
			if err := tx.ReplaceSessionFiles(sid, []model.FileTouch{{Path: "/repo/wt/a.go", TS: testNow.Add(-time.Minute)}, {Path: "/repo/wt/b.go", TS: testNow}}); err != nil {
				return err
			}
			kind := "finished"
			if id == "waiting" {
				kind = "waiting"
			}
			if err := tx.UpsertInboxItem(model.InboxItem{ID: id, SessionID: sid, Kind: kind, TS: testNow.Add(-time.Minute), Reason: "question"}); err != nil {
				return err
			}
		}
		return nil
	})
	v, err := Inbox(s, Filter{}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	var done *InboxRow
	for i := range v.Rows {
		switch v.Rows[i].ID {
		case "done":
			done = &v.Rows[i]
		case "waiting":
			if v.Rows[i].Checkout != nil || v.Rows[i].Files != nil {
				t.Fatalf("a wait has nothing to review: %+v", v.Rows[i])
			}
		}
	}
	if done == nil {
		t.Fatalf("finished turn missing: %+v", v.Rows)
	}
	if c := done.Checkout; c == nil || c.Path != "/repo/wt" || c.Dirty != 2 || c.Untracked != 1 || c.Ahead != 3 || c.LastCommitSubject != "add review" {
		t.Fatalf("checkout = %+v", done.Checkout)
	}
	if !slices.Equal(done.Files, []string{"/repo/wt/a.go", "/repo/wt/b.go"}) {
		t.Fatalf("files = %v", done.Files)
	}
}
