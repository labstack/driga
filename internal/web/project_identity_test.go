package web

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestSameNamedProjectsStaySeparate(t *testing.T) {
	s, st, _, _ := fixture(t)
	err := st.Tx(context.Background(), func(tx *store.Tx) error {
		for i, path := range []string{"/a/app", "/b/app"} {
			id, err := tx.UpsertRepo(model.Repo{Name: "app", Path: path})
			if err != nil {
				return err
			}
			_, err = tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: path, Branch: fmt.Sprint("branch", i), Title: fmt.Sprint("title", i), LastActivityAt: time.Now()}, RepoID: &id, Label: "working"})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := s.loadPage(httptest.NewRequest("GET", "http://"+s.opts.Host+"/", nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, card := range p.ProjectCards {
		if card.Status.Repo.Name == "app" {
			want := "branch0"
			if card.Status.Repo.Path == "/b/app" {
				want = "branch1"
			}
			if card.Branch != want || len(card.States) != 1 || card.States[0].Count != 1 {
				t.Fatalf("mixed: %+v", card)
			}
		}
	}
}
