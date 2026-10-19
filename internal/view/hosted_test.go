package view

import (
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestHostedIdleSessionsHiddenAcrossViews(t *testing.T) {
	s := fixture(t, func(tx *store.Tx) error {
		repo, err := tx.UpsertRepo(model.Repo{Name: "repo", Path: "/repo"})
		if err != nil {
			return err
		}
		for _, obs := range []model.SessionObs{
			{ExternalID: "hosted-idle", Hosted: true, LastActivityAt: testNow.Add(-2 * time.Hour)},
			{ExternalID: "origin-idle", Origin: "hosted/desktop", LastActivityAt: testNow.Add(-2 * time.Hour)},
			{ExternalID: "hosted-active", Hosted: true, LastActivityAt: testNow.Add(-5 * time.Minute)},
			{ExternalID: "hosted-boundary", Hosted: true, LastActivityAt: testNow.Add(-time.Hour)},
			{ExternalID: "local-idle", LastActivityAt: testNow.Add(-48 * time.Hour)},
		} {
			obs.Adapter, obs.Cwd = "test", "/repo"
			if _, err := tx.UpsertSession(model.Session{SessionObs: obs, RepoID: &repo, Label: "idle"}); err != nil {
				return err
			}
		}
		return nil
	})
	for _, all := range []bool{false, true} {
		v, err := Agents(s, AgentFilter{All: all}, testNow)
		if err != nil {
			t.Fatal(err)
		}
		want := 3
		if all {
			want = 5
		}
		if len(v.Rows) != want {
			t.Errorf("all=%t: got %d agents, want %d", all, len(v.Rows), want)
		}
	}
	summary, err := Summary(s, Filter{}, testNow)
	if err != nil || summary.Agents != 3 {
		t.Errorf("summary: %+v, %v", summary, err)
	}
	projects, err := Projects(s, Filter{}, testNow)
	if err != nil || len(projects.Rows) != 1 || projects.Rows[0].Status.LiveAgents != 3 {
		t.Errorf("projects: %+v, %v", projects, err)
	}
	where, err := Where(s, "/repo", testNow)
	if err != nil || where.Status.LiveAgents != 3 || len(where.Agents.Rows) != 3 {
		t.Errorf("where: %+v, %v", where, err)
	}
}
