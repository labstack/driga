package store

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
)

func TestNeedsYouMigrationPreservesHistoryAndWorkerIdentity(t *testing.T) {
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
	if _, err = p.UpTo(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO meta VALUES('old','machine_id','old');
 INSERT INTO sessions (machine_id,adapter,external_id,origin,version,pid,proc_start,cwd,branch,title,last_prompt,registry_status,state,state_heuristic,evidence,started_at,last_activity_at,hosted,turn_open,source_path) VALUES('old','claude','parent','','',0,0,'','','','','','idle',0,'',0,0,0,0,''),('old','claude','worker','','',0,0,'','','','','','idle',0,'',0,0,0,0,'');
 INSERT INTO inbox_items(machine_id,id,session_id,kind,ts,summary) VALUES('old','claude:worker:100',1,'waiting',100,'first'),('old','claude:worker:200',1,'waiting',200,'repeat');`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	st := openTest(t, path, ReadWrite)
	rows, err := st.Inbox(time.Time{}, false)
	if err != nil || len(rows) != 1 || rows[0].SessionID != 2 || rows[0].ID != "claude:worker:100" || rows[0].Reason != "unknown" {
		t.Fatalf("migration active waits: %+v %v", rows, err)
	}
	history, err := st.Inbox(time.Time{}, true)
	if err != nil || len(history) != 2 {
		t.Fatal(history, err)
	}
	for _, i := range history {
		if i.AckedAt != nil {
			t.Fatal("migration manufactured ack", i)
		}
	}
}
