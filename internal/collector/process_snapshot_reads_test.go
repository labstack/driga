package collector

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/labstack/driga/internal/store"
)

func TestPassWithFreshProcsSkipsStoredProcs(t *testing.T) {
	d, _, _, _ := setup(t)
	path := filepath.Join(t.TempDir(), "db")
	st, err := store.Open(path, store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO procs (machine_id,pid,ppid,start_time,rss,cpu_seconds,cpu_percent,cmdline,argv,exe,role,agent,session_id,cwd) VALUES (?,'unreadable',0,0,0,0,0,'','[]','','','',0,'')`, st.MachineID()); err != nil {
		t.Fatal(err)
	}
	pass(t, d, st, Plan{Procs: true})
	ps, err := st.Procs()
	if err != nil || len(ps) != 1 || ps[0].PID != 101 {
		t.Fatalf("procs: %+v %v", ps, err)
	}
	// Later passes without a scan reuse the rows this run wrote, not the DB.
	if _, err := db.Exec(`UPDATE procs SET pid='unreadable'`); err != nil {
		t.Fatal(err)
	}
	pass(t, d, st, Plan{})
	if ss := sessions(t, st); len(ss) != 1 || ss[0].PID != 101 || ss[0].Label != "working" {
		t.Fatalf("sessions after unscanned pass: %+v", ss)
	}
}
