package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSettingsBackupRetention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		d, err := readDiff(path)
		if err != nil {
			t.Fatal(err)
		}
		d.Changed = true
		d.After = fmt.Sprintf("{\"generation\":%d}", i)
		if err := writeDiff(d, false, time.Unix(1800000000+int64(i), 0)); err != nil {
			t.Fatal(err)
		}
		files, _ := filepath.Glob(path + ".driga-bak-*")
		if len(files) != min(i+1, 5) {
			t.Fatalf("backup count %d after %d writes", len(files), i+1)
		}
	}
	files, _ := filepath.Glob(path + ".driga-bak-*")
	for i, path := range files {
		if filepath.Base(path) != fmt.Sprintf("settings.json.driga-bak-%d", 1800000003+i) {
			t.Fatal(files)
		}
	}
}
