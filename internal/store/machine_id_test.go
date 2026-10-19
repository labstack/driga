package store

import (
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestMachineIdentityUUIDv7AndLegacyPreservation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "driga.db")
	st, err := Open(path, ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	id := st.MachineID()
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.Version() != 7 || parsed.String() != id {
		t.Fatalf("machine ID %q: %v", id, err)
	}
	// Simulate an installed machine's pre-UUID ID in its original metadata row.
	legacy := "0123456789abcdef0123456789abcdef"
	if _, err := st.db.Exec("UPDATE meta SET value=? WHERE key='machine_id'", legacy); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		st, err = Open(path, ReadWrite)
		if err != nil {
			t.Fatal(err)
		}
		if st.MachineID() != legacy {
			t.Fatal("installed identity regenerated", st.MachineID())
		}
		st.Close()
	}
}
