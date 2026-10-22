package store

import (
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestMachineIdentityUUIDv7Persists(t *testing.T) {
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
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		st, err = Open(path, ReadWrite)
		if err != nil {
			t.Fatal(err)
		}
		if st.MachineID() != id {
			t.Fatal("installed identity regenerated", st.MachineID())
		}
		st.Close()
	}
}

func TestInvalidMachineIdentityIsRefused(t *testing.T) {
	for _, id := range []string{"", "0123456789abcdef0123456789abcdef", uuid.NewString(), "019a0000-0000-7000-0000-000000000000"} {
		t.Run(id, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "driga.db")
			st, err := Open(path, ReadWrite)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.db.Exec("UPDATE meta SET value=? WHERE key='machine_id'", id); err != nil {
				t.Fatal(err)
			}
			st.Close()
			for _, mode := range []Mode{ReadOnly, ReadWrite} {
				reopened, err := Open(path, mode)
				if err == nil {
					reopened.Close()
					t.Fatal("invalid machine identity accepted")
				}
			}
		})
	}
}
