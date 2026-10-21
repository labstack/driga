package store

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestStartedFromMigrationAndImmutableUpsert(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	sub, _ := fs.Sub(migrations, "migrations")
	p, err := goose.NewProvider(goose.DialectSQLite3, db, sub, goose.WithDisableGlobalRegistry(true), goose.WithGoMigrations(canonMigration()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.UpTo(context.Background(), 14); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO meta VALUES('old','machine_id','old');
INSERT INTO sessions(machine_id,adapter,external_id,origin,version,pid,proc_start,cwd,branch,title,last_prompt,registry_status,state,state_heuristic,evidence,started_at,last_activity_at,hosted,turn_open,source_path) VALUES('old','claude','session','','',0,0,'','','Task','','','idle',0,'',0,0,0,0,'');`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	st := openTest(t, path, ReadWrite)
	s, err := st.Session(1)
	if err != nil || s.Title != "Task" || s.StartedFrom != "" {
		t.Fatalf("legacy session: %+v %v", s, err)
	}
	var isNull bool
	if err := st.conn.QueryRowContext(context.Background(), "SELECT started_from IS NULL FROM sessions WHERE id=1").Scan(&isNull); err != nil || !isNull {
		t.Fatalf("nullable migration: %t %v", isNull, err)
	}
	for _, surface := range []string{"Fixture Terminal", "", "Different Editor"} {
		s.StartedFrom = surface
		mustTx(t, st, func(tx *Tx) error { _, err := tx.UpsertSession(s); return err })
		got, err := st.Session(s.ID)
		if err != nil || got.StartedFrom != "Fixture Terminal" {
			t.Fatalf("upsert %q: %+v %v", surface, got, err)
		}
	}
	// A remote session uses the same invariant, even when an older peer omits it.
	mustTx(t, st, func(tx *Tx) error {
		peer := tx.ForMachine("peer")
		for _, surface := range []string{"Fixture Editor", "", "Changed"} {
			s.StartedFrom = surface
			if _, err := peer.UpsertSession(s); err != nil {
				return err
			}
		}
		return nil
	})
	ss, err := st.SessionsScoped(SessionFilter{}, Scope{Machines: []string{"peer"}})
	if err != nil || len(ss) != 1 || ss[0].StartedFrom != "Fixture Editor" {
		t.Fatalf("peer: %+v %v", ss, err)
	}
}
