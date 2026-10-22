package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

// A reader must explain an older schema (an upgraded binary while the old
// collector still runs) instead of failing later with "no such column".
func TestReadOnlyRejectsOlderSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "driga.db")
	st, err := Open(path, ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	ro, err := Open(path, ReadOnly)
	if err != nil {
		t.Fatalf("current schema: %v", err)
	}
	ro.Close()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DELETE FROM goose_db_version WHERE version_id = (SELECT MAX(version_id) FROM goose_db_version)"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := Open(path, ReadOnly); !errors.Is(err, ErrSchemaBehind) {
		t.Fatalf("older schema: %v", err)
	}
}
