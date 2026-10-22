package collector

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/labstack/driga/internal/fixturegen"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestGitInventoryReconcilesRemovedWorktrees(t *testing.T) {
	d, st, _, a := setup(t)
	home := os.Getenv("HOME")
	if err := fixturegen.Write(home, fixturegen.Scenario{Now: d.Now(), Repos: 1, WorktreesPerRepo: 3}); err != nil {
		t.Fatal(err)
	}
	materializeFixture(t, home)
	repo := filepath.Join(home, "groves", "grove-000")
	d.Cfg.Roots, a.obs = []string{repo}, nil
	pass(t, d, st, Plan{Git: true, GitFull: true})
	ws, err := st.Worktrees(0)
	if err != nil || len(ws) != 3 {
		t.Fatalf("initial inventory: %+v %v", ws, err)
	}
	var sessionID int64
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		var err error
		at := d.Now()
		sessionID, err = tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "history"}, WorktreeID: &ws[1].ID, RepoID: &ws[1].RepoID, EndedAt: &at})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// One checkout is deleted but still registered; another is unregistered
	// while its directory remains. Neither belongs to current inventory.
	if err := os.RemoveAll(repo + "-branch-001"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(repo, ".git", "worktrees", "branch-002")); err != nil {
		t.Fatal(err)
	}
	// Once discovered, inventory must refresh even without an active session
	// or a configured root pointing at this repository on the next pass.
	d.Cfg.Roots = nil
	pass(t, d, st, Plan{Git: true})
	ws, err = st.Worktrees(0)
	if err != nil || len(ws) != 1 {
		t.Fatalf("removed worktrees remain: %+v %v", ws, err)
	}
	s, err := st.Session(sessionID)
	if err != nil || s.WorktreeID != nil {
		t.Fatalf("historical session reference: %+v %v", s, err)
	}
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}
	pass(t, d, st, Plan{Git: true})
	ws, err = st.Worktrees(0)
	if err != nil || len(ws) != 0 {
		t.Fatalf("deleted repo retained current inventory: %+v %v", ws, err)
	}
}

func TestGitInventoryPreservesFailedRepos(t *testing.T) {
	_, st, _, _ := setup(t)
	err := st.Tx(context.Background(), func(tx *store.Tx) error {
		for _, path := range []string{"failed", "empty"} {
			id, err := tx.UpsertRepo(model.Repo{Path: path})
			if err != nil {
				return err
			}
			if _, err := tx.UpsertWorktree(model.Worktree{RepoID: id, Path: path}); err != nil {
				return err
			}
		}
		return writeGit(tx, []repoResult{{path: "failed", err: errors.New("status denied")}, {path: "empty"}}, errors.New("status denied"), func(string, error) error { return nil }, &PassReport{})
	})
	if err != nil {
		t.Fatal(err)
	}
	ws, err := st.Worktrees(0)
	if err != nil || len(ws) != 1 || ws[0].Path != "failed" {
		t.Fatalf("partial scan lost inventory: %+v %v", ws, err)
	}
}
