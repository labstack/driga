package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

// SyncProcs leaves the same table as ReplaceProcs while rewriting only the
// rows that changed since prev, the table it is given as current.
func TestSyncProcsWritesOnlyChangedRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	st, err := Open(path, ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	start := time.Unix(1700000000, 0).UTC()
	prev := []model.Proc{
		{PID: 1, PPID: 0, StartTime: start, RSS: 10, Cmdline: "same", Argv: []string{"same"}},
		{PID: 2, PPID: 1, StartTime: start, RSS: 20, Cmdline: "changes", Argv: []string{"changes"}},
		{PID: 3, PPID: 1, StartTime: start, RSS: 30, Cmdline: "exits", Argv: []string{"exits"}},
		{PID: 4, PPID: 1, StartTime: start, RSS: 40, Cmdline: "pid reused", Argv: []string{"pid", "reused"}},
	}
	next := []model.Proc{
		{PID: 1, PPID: 0, StartTime: start, RSS: 10, Cmdline: "same", Argv: []string{"same"}},
		{PID: 2, PPID: 1, StartTime: start, RSS: 25, Cmdline: "changes", Argv: []string{"changes"}, CPUPercent: 3},
		{PID: 4, PPID: 1, StartTime: start.Add(time.Hour), RSS: 41, Cmdline: "new process", Argv: []string{"new", "process"}},
		{PID: 5, PPID: 1, StartTime: start, RSS: 50, Cmdline: "started", Argv: []string{"started"}, SessionID: 9, Cwd: "/w"},
	}
	ctx := context.Background()
	if err := st.Tx(ctx, func(tx *Tx) error { return tx.ReplaceProcs(prev) }); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE writes(n INTEGER); INSERT INTO writes VALUES(0);
		CREATE TRIGGER count_insert AFTER INSERT ON procs BEGIN UPDATE writes SET n=n+1; END;
		CREATE TRIGGER count_update AFTER UPDATE ON procs BEGIN UPDATE writes SET n=n+1; END;
		CREATE TRIGGER count_delete AFTER DELETE ON procs BEGIN UPDATE writes SET n=n+1; END;`); err != nil {
		t.Fatal(err)
	}
	if err := st.Tx(ctx, func(tx *Tx) error { return tx.SyncProcs(prev, next) }); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow("SELECT n FROM writes").Scan(&n); err != nil {
		t.Fatal(err)
	}
	// pid 2 rewritten, pid 3 deleted, pid 4 rewritten, pid 5 inserted; pid 1 untouched.
	if n > 4 {
		t.Fatalf("SyncProcs made %d row writes, want at most 4", n)
	}
	got, err := st.Procs()
	if err != nil {
		t.Fatal(err)
	}
	for i := range got {
		got[i].MachineID = ""
	}
	if !reflect.DeepEqual(got, next) {
		t.Fatalf("table after sync:\n got %+v\nwant %+v", got, next)
	}
}
