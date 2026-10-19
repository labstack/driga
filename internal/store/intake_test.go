package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/pressly/goose/v3"
)

func TestMigration0004PreservesSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	fs := fstest.MapFS{}
	for _, name := range []string{"0001_init.sql", "0002_orphan_verified.sql", "0003_fleet.sql"} {
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
	row, err := db.Exec(`INSERT INTO sessions(machine_id,adapter,external_id,origin,version,pid,proc_start,cwd,branch,title,last_prompt,registry_status,state,state_heuristic,evidence,started_at,last_activity_at,hosted,turn_open,source_path) VALUES('old','claude','existing','','',0,0,'','','','','','idle',0,'',0,0,0,0,'')`)
	var id int64
	if err == nil {
		id, err = row.LastInsertId()
	}
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	st, err := Open(path, ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s, err := st.Session(id)
	if err != nil || s.ExternalID != "existing" || s.Label != "idle" {
		t.Fatal(s, err)
	}
	state, err := st.IntakeState(id)
	if err != nil || !state.LastDecision.IsZero() || !state.LastAPIRequest.IsZero() || state.LastAPIError != "" {
		t.Fatal(state, err)
	}
	want := IntakeState{LastDecision: time.Unix(1792324800, 0).UTC(), LastAPIRequest: time.Unix(1792324801, 0).UTC(), LastAPIError: "API error 529, attempt 3"}
	if err := st.Tx(context.Background(), func(tx *Tx) error {
		if err := tx.SetIntakeState(id, want); err != nil {
			return err
		}
		_, err := tx.UpsertSession(s)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	got, err := st.IntakeState(id)
	if err != nil || got != want {
		t.Fatal(got, err)
	}
}
