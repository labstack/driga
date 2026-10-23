package collector

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestPassScansGitForResumedSession(t *testing.T) {
	d, st, _, a := setup(t)
	repo := t.TempDir()
	if out, err := exec.Command("git", "init", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %s %v", out, err)
	}
	old := d.Now().Add(-8 * 24 * time.Hour)
	a.obs[0].Cwd, a.obs[0].LastActivityAt = repo, old
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		_, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "one", Cwd: repo, LastActivityAt: old}, EndedAt: &old, Label: "dead"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	pass(t, d, st, Plan{Procs: true, Git: true, GitFull: true})
	if s := sessions(t, st)[0]; s.EndedAt != nil || s.RepoID == nil {
		t.Fatalf("resumed session omitted from git: %+v", s)
	}
}
