package collector

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/labstack/driga/internal/store"
)

func TestPassUpsertsOnlyChangedSessions(t *testing.T) {
	d, _, _, a := setup(t)
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
	_, err = db.Exec(`CREATE TABLE writes(n INTEGER); INSERT INTO writes VALUES(0); CREATE TRIGGER count_updates AFTER UPDATE ON sessions BEGIN UPDATE writes SET n=n+1; END;`)
	if err != nil {
		t.Fatal(err)
	}
	pass(t, d, st, Plan{Procs: true, Sockets: true})
	if _, err = db.Exec("UPDATE writes SET n=0"); err != nil {
		t.Fatal(err)
	}
	pass(t, d, st, Plan{})
	var n int
	if err := db.QueryRow("SELECT n FROM writes").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("unchanged session updated %d times", n)
	}
	a.obs[0].Title = "changed"
	pass(t, d, st, Plan{})
	if err := db.QueryRow("SELECT n FROM writes").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("changed session writes=%d", n)
	}
}
