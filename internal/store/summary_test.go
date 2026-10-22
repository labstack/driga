package store

import (
	"context"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestSummaryRoundTrip(t *testing.T) {
	st := openTest(t, "", Memory)
	var repo int64
	mustTx(t, st, func(tx *Tx) error {
		var err error
		repo, err = tx.UpsertRepo(model.Repo{Name: "fixture", Path: "/fixture"})
		return err
	})
	if err := st.Tx(context.Background(), func(tx *Tx) error {
		for _, used := range []bool{false, true} {
			if err := tx.UpsertSummary(model.Summary{ProjectID: repo, InputHash: "input", StateHash: "state", Text: "sentence", Model: "haiku", UsedSnippets: used, CreatedAt: time.Now()}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := st.Summaries(repo)
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	repos, err := st.Repos()
	if err != nil || len(repos) != 1 || repos[0].Name != "fixture" {
		t.Fatal(repos, err)
	}
}
