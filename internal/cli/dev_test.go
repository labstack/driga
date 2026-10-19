package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	_ "modernc.org/sqlite"
)

func TestAgySchemaPrintsNamesOnly(t *testing.T) {
	g, out := initFixture(t, "")
	dir := filepath.Join(t.TempDir(), "conversations")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	paths := []string{filepath.Join(dir, "one.db"), filepath.Join(dir, "two.db")}
	for _, path := range paths {
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.Exec(`CREATE TABLE messages(id INTEGER PRIMARY KEY, body TEXT DEFAULT 'SECRET-VALUE'); INSERT INTO messages(body) VALUES ('SECRET-VALUE');`)
		closeErr := db.Close()
		if err != nil {
			t.Fatal(err)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if err := (DevAgySchemaCmd{Dir: dir}).Run(g); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"schema", "2 databases", "messages", "id", "INTEGER", "body", "TEXT", "rows=2"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	if strings.Count(text, "messages") != 1 || strings.Contains(text, "SECRET-VALUE") {
		t.Fatal(text)
	}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || !info.ModTime().Equal(old) {
			t.Fatalf("mtime: %+v %v", info, err)
		}
		db, err := openAgySchemaDB(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(`CREATE TABLE forbidden(id INTEGER)`); err == nil {
			db.Close()
			t.Fatal("database not read-only")
		}
		db.Close()
	}
	// Verify the hidden command is reachable and honors the JSON envelope.
	out.Reset()
	var stderr bytes.Buffer
	if code := run([]string{"dev", "agy-schema", "--dir", dir, "--json"}, "test", strings.NewReader(""), out, &stderr); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, &stderr)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"schema", "observed_at", "mode", "freshness", "coverage_gaps", "data"} {
		if envelope[key] == nil {
			t.Fatalf("missing %s: %s", key, out)
		}
	}
	if strings.Contains(out.String(), "SECRET-VALUE") {
		t.Fatal(out)
	}
}

func TestDoctorAgyLine(t *testing.T) {
	for _, source := range []string{"directory", "process", "absent"} {
		t.Run(source, func(t *testing.T) {
			d, st := doctorFixture(t)
			if source == "directory" {
				if err := os.MkdirAll(filepath.Join(d.Home, ".gemini", "antigravity-cli"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if source == "process" {
				if err := st.Tx(context.Background(), func(tx *store.Tx) error {
					return tx.ReplaceProcs([]model.Proc{{PID: 42, Exe: "/opt/bin/agy", Cmdline: "/opt/bin/agy"}})
				}); err != nil {
					t.Fatal(err)
				}
			}
			checks := Doctor(context.Background(), d)
			if source == "absent" {
				for _, c := range checks {
					if c.Name == "agy" {
						t.Fatal(c)
					}
				}
				return
			}
			c := requireCheck(t, checks, "agy", "info", "")
			if c.Detail != "process-level (schema not yet supported)" {
				t.Fatal(c)
			}
		})
	}
}

func TestAgySchemaSourceWarning(t *testing.T) {
	g, out := initFixture(t, "")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.db"), []byte("synthetic invalid database SECRET-VALUE"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := (DevAgySchemaCmd{Dir: dir}).Run(g); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "source: cannot inspect broken.db") || strings.Contains(out.String(), "SECRET-VALUE") {
		t.Fatal(out)
	}
	path := filepath.Join(dir, "locked.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE sample(id INTEGER); BEGIN EXCLUSIVE"); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("ROLLBACK")
	out.Reset()
	if err := (DevAgySchemaCmd{Dir: dir}).Run(g); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "source: cannot inspect locked.db") {
		t.Fatal(out)
	}
}
