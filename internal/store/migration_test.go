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

func TestOrphanVerifiedMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := migrations.ReadFile("migrations/0001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, db, fstest.MapFS{"0001_init.sql": &fstest.MapFile{Data: b}}, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO meta VALUES('old','machine_id','old'); INSERT INTO orphans(machine_id,id,root_pid,start_time,signature,proc_count,rss_total,cwd,young,evidence,pids,first_seen) VALUES('old','candidate',2,0,'helper',1,0,'',0,'[]','[2]',0)`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	st, err := Open(path, ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var verified int
	if err := st.db.QueryRow("SELECT verified FROM orphans").Scan(&verified); err != nil {
		t.Fatal(err)
	}
	if verified != 0 {
		t.Fatal("old candidate became verified")
	}
	o := model.Orphan{ID: "candidate", RootPID: 2, PIDs: []int32{2}, Verified: true, FirstSeen: time.Now()}
	if err := st.Tx(context.Background(), func(tx *Tx) error { return tx.ReplaceOrphans([]model.Orphan{o}) }); err != nil {
		t.Fatal(err)
	}
	os, err := st.Orphans()
	if err != nil || len(os) != 1 || !os[0].Verified {
		t.Fatalf("verified round-trip: %+v %v", os, err)
	}
}
