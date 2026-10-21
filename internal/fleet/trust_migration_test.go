package fleet

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestViewerContractMigration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "viewers.json")
	if err := os.WriteFile(path, []byte(`[{"device_id":"preserved-id","last_connected":"2026-10-19T12:00:00-07:00","permissions":["read"]}]`), 0600); err != nil {
		t.Fatal(err)
	}
	viewers, err := LoadViewers(dir)
	if err != nil || len(viewers) != 1 || viewers[0].LastConnected.IsZero() {
		t.Fatalf("%+v %v", viewers, err)
	}
	b, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(b), `"last_connected"`) || !strings.Contains(string(b), `"last_connected_at"`) {
		t.Fatalf("%s %v", b, err)
	}
	if !strings.Contains(string(b), "2026-10-19T19:00:00Z") || strings.Contains(string(b), "-07:00") {
		t.Fatalf("persisted migration did not normalize UTC: %s", b)
	}
	before := string(b)
	if _, err = LoadViewers(dir); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if string(b) != before {
		t.Fatal("migration is not idempotent")
	}
	b, err = json.Marshal(viewers)
	if err != nil || !strings.Contains(string(b), `2026-10-19T19:00:00Z`) {
		t.Fatalf("%s %v", b, err)
	}
}
