package cli

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/store"
)

func TestDoctorReportsSchemaReset(t *testing.T) {
	d, _ := doctorFixture(t)
	t.Setenv("DRIGA_DATA_DIR", d.DataDir)
	configPath := filepath.Join(d.Home, "config.toml")
	t.Setenv("DRIGA_CONFIG", configPath)
	if err := os.WriteFile(configPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(d.DataDir, "driga.db")
	st, err := store.Open(path, store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA user_version=0"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	_, schemaErr := store.Open(path, store.ReadOnly)
	if schemaErr == nil {
		t.Fatal("expected pre-release schema")
	}
	var out bytes.Buffer
	err = (DoctorCmd{}).Run(&Globals{Stdout: &out})
	if err == nil {
		t.Fatal("doctor accepted incompatible database")
	}
	t.Logf("doctor error: %v", err)
	if !strings.Contains(out.String(), schemaErr.Error()) {
		t.Fatalf("doctor omitted shared schema diagnosis: %s", out.String())
	}
}
