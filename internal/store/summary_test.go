package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/pressly/goose/v3"
)

func TestMigration0005(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	fs := fstest.MapFS{}
	for _, name := range []string{"0001_init.sql", "0002_orphan_verified.sql", "0003_fleet.sql", "0004_intake.sql"} {
		b, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		fs[name] = &fstest.MapFile{Data: b}
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, db, fs, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO meta VALUES ('old','machine_id','old')"); err != nil {
		t.Fatal(err)
	}
	q := &Tx{querier: querier{conn: db, ctx: context.Background(), machineID: "old"}}
	repo, err := q.UpsertRepo(model.Repo{Name: "fixture", Path: "/fixture"})
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	st, err := Open(path, ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
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
