package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/model"
)

func TestFreshBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "driga.db")
	st := openTest(t, path, ReadWrite)
	rows, err := st.db.Query("SELECT version_id FROM goose_db_version WHERE is_applied=1 ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	var versions []int
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		versions = append(versions, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if !reflect.DeepEqual(versions, []int{0, 1, 2}) {
		t.Fatal(versions)
	}
	for pragma, want := range map[string]int{"user_version": 1, "auto_vacuum": 2, "foreign_keys": 1} {
		var got int
		if err := st.db.QueryRow("PRAGMA " + pragma).Scan(&got); err != nil || got != want {
			t.Fatalf("%s=%d want %d: %v", pragma, got, want, err)
		}
	}
	fk, err := st.db.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	if fk.Next() {
		t.Fatal("baseline has invalid foreign keys")
	}
	if err := fk.Err(); err != nil {
		t.Fatal(err)
	}
	fk.Close()
	for _, mode := range []Mode{ReadOnly, ReadWrite} {
		other := openTest(t, path, mode)
		if other.MachineID() != st.MachineID() {
			t.Fatal("identity changed")
		}
	}
}

func TestRejectPreReleaseHistory(t *testing.T) {
	for _, fixture := range []string{"old-chain", "old-version-one", "unknown-chain", "missing-history", "rolled-back", "duplicate", "unmarked-baseline"} {
		t.Run(fixture, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "data with spaces", "driga.db")
			st, err := Open(path, ReadWrite)
			if err != nil {
				t.Fatal(err)
			}
			if err := st.Close(); err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			switch fixture {
			case "old-chain", "old-version-one":
				if _, err := db.Exec("PRAGMA user_version=0; DELETE FROM goose_db_version"); err != nil {
					t.Fatal(err)
				}
				last := 20
				if fixture == "old-version-one" {
					last = 1
				}
				for v := 1; v <= last; v++ {
					if _, err := db.Exec("INSERT INTO goose_db_version(version_id,is_applied) VALUES (?,1)", v); err != nil {
						t.Fatal(err)
					}
				}
			case "unknown-chain":
				if _, err := db.Exec("DELETE FROM goose_db_version WHERE version_id=0"); err != nil {
					t.Fatal(err)
				}
			case "missing-history":
				if _, err := db.Exec("DROP TABLE goose_db_version"); err != nil {
					t.Fatal(err)
				}
			case "rolled-back":
				if _, err := db.Exec("INSERT INTO goose_db_version(version_id,is_applied) VALUES (1,0)"); err != nil {
					t.Fatal(err)
				}
			case "duplicate":
				if _, err := db.Exec("INSERT INTO goose_db_version(version_id,is_applied) VALUES (1,1)"); err != nil {
					t.Fatal(err)
				}
			case "unmarked-baseline":
				if _, err := db.Exec("PRAGMA user_version=0"); err != nil {
					t.Fatal(err)
				}
			}
			for _, mode := range []Mode{ReadWrite, ReadOnly} {
				st, err := Open(path, mode)
				if st != nil {
					st.Close()
					t.Fatal("incompatible database opened")
				}
				if !errors.Is(err, ErrSchemaPreRelease) {
					t.Fatalf("mode %d: %v", mode, err)
				}
				want := (&SchemaError{Path: path}).Error()
				if err.Error() != want {
					t.Fatalf("got %q want %q", err, want)
				}
				for _, text := range []string{path, path + "-wal", path + "-shm", "driga service stop", "driga service start"} {
					if !strings.Contains(err.Error(), text) {
						t.Fatalf("missing %q: %v", text, err)
					}
				}
			}
			// Refusal must preserve the existing data and schema marker.
			var id string
			if err := db.QueryRow("SELECT value FROM meta WHERE key='machine_id'").Scan(&id); err != nil || id == "" {
				t.Fatal(id, err)
			}
		})
	}
}

func TestRejectNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "driga.db")
	st, err := Open(path, ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec("INSERT INTO goose_db_version(version_id,is_applied) VALUES (99,1)"); err != nil {
		t.Fatal(err)
	}
	st.Close()
	for _, mode := range []Mode{ReadOnly, ReadWrite} {
		st, err := Open(path, mode)
		if st != nil {
			st.Close()
			t.Fatal("newer database opened")
		}
		if !errors.Is(err, ErrSchemaNewer) || !strings.Contains(err.Error(), "upgrade driga") || strings.Contains(err.Error(), "reset it") {
			t.Fatalf("mode %d: %v", mode, err)
		}
	}
}

func TestReadOnlySchemaCheckDoesNotCreateHistory(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "empty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, _, err := checkSchema(context.Background(), db, "empty.db"); !errors.Is(err, ErrSchemaPreRelease) {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_schema").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}

func TestResetCommandsQuoteExactFiles(t *testing.T) {
	path := "/data/it's a database.db"
	posix := resetCommand(path, "linux")
	if !strings.Contains(posix, `'/data/it'"'"'s a database.db-wal'`) || strings.Contains(posix, "*") {
		t.Fatal(posix)
	}
	windows := resetCommand(`C:\data\it's a database.db`, "windows")
	for _, part := range []string{"Remove-Item -LiteralPath", "'C:\\data\\it''s a database.db'", "database.db-wal'", "database.db-shm'", "$LASTEXITCODE -eq 0"} {
		if !strings.Contains(windows, part) {
			t.Fatalf("missing %q: %s", part, windows)
		}
	}
}

func TestLocalMachineAlwaysRecordsOS(t *testing.T) {
	st := openTest(t, "", Memory)
	mustTx(t, st, func(tx *Tx) error { return tx.UpsertMachine(model.Machine{Hostname: "local"}) })
	m, err := st.Machine()
	if err != nil || m.OS != runtime.GOOS {
		t.Fatalf("missing local provenance: %+v %v", m, err)
	}
}

// A database created at the baseline keeps its data: the collector applies
// forward migrations, while a reader asks for the collector to restart.
func TestForwardMigrationFromBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "driga.db")
	st, err := Open(path, ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	id := st.machineID
	st.Close()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// Rewind to a baseline-only database, as created by the previous release.
	for _, stmt := range []string{"ALTER TABLE procs DROP COLUMN argv", "DELETE FROM goose_db_version WHERE version_id > 1"} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	if st, err := Open(path, ReadOnly); !errors.Is(err, ErrSchemaBehind) || !strings.Contains(err.Error(), "driga service restart") {
		if st != nil {
			st.Close()
		}
		t.Fatalf("reader: %v", err)
	}
	st, err = Open(path, ReadWrite)
	if err != nil {
		t.Fatalf("writer migrate: %v", err)
	}
	defer st.Close()
	if st.machineID != id {
		t.Fatal("machine identity lost in migration")
	}
	var argv string
	if _, err := st.db.Exec("INSERT INTO procs (machine_id,pid,ppid,start_time,rss,cpu_seconds,cpu_percent,cmdline,exe,role,agent,session_id,cwd,tty,footprint,swap,memory_measure,partial) VALUES (?,1,0,0,0,0,0,'x','x','','','','','',0,0,'',0)", id); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRow("SELECT argv FROM procs WHERE pid=1").Scan(&argv); err != nil || argv != "[]" {
		t.Fatal(argv, err)
	}
	if st, err := Open(path, ReadOnly); err != nil {
		t.Fatalf("reader after migration: %v", err)
	} else {
		st.Close()
	}
}
