package web

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestBranchColumnDoesNotWrap(t *testing.T) {
	s, st, agent, _ := fixture(t)
	err := st.Tx(context.Background(), func(tx *store.Tx) error {
		session, err := tx.Session(agent)
		if err != nil {
			return err
		}
		session.Branch = "feature/a-long-branch-name-with-many-segments"
		_, err = tx.UpsertSession(session)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	body := request(s, "GET", "/projects", s.token).Body.String()
	if !strings.Contains(body, `class="mono branch ag" title="feature/a-long-branch-name-with-many-segments"`) {
		t.Fatal("branch lacks full title")
	}
	css := request(s, "GET", "/static/app.css", s.token).Body.String()
	if !strings.Contains(css, ".branch") || !strings.Contains(css, "white-space: nowrap") || !strings.Contains(css, "text-overflow: ellipsis") {
		t.Fatal("missing truncation styles")
	}
}

func TestOverviewKeepsQuietProjectBranch(t *testing.T) {
	s, st, _, _ := fixture(t)
	err := st.Tx(context.Background(), func(tx *store.Tx) error {
		id, err := tx.UpsertRepo(model.Repo{Name: "Quiet checkout", Path: "/quiet"})
		if err != nil {
			return err
		}
		_, err = tx.UpsertWorktree(model.Worktree{RepoID: id, Path: "/quiet", Branch: "quiet-main"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := s.loadPage(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, card := range p.ProjectCards {
		if card.Status.Repo.Path == "/quiet" && card.Branch != "quiet-main" {
			t.Fatalf("quiet checkout lost branch: %+v", card)
		}
	}
}
