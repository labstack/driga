package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/cleanup"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/platform"
)

func cleanupFake(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "procs.json"), []byte("[]"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "machine.json"), []byte(`{"os":"linux"}`), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := platform.LoadFake(dir)
	if err != nil {
		t.Fatal(err)
	}
	old := cleanupPlatform
	cleanupPlatform = func() platform.Platform { return p }
	t.Cleanup(func() { cleanupPlatform = old })
}
func TestCleanupListJSON(t *testing.T) {
	readEnv(t)
	cleanupFake(t)
	readDB(t, nil)
	before, err := os.ReadFile(filepath.Join(os.Getenv("DRIGA_DATA_DIR"), "driga.db"))
	if err != nil {
		t.Fatal(err)
	}
	out, _ := readRun(t, "cleanup", "--json")
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(out, err)
	}
	for _, key := range []string{"schema", "observed_at", "mode", "freshness", "coverage_gaps", "data"} {
		if _, ok := envelope[key]; !ok {
			t.Fatal(key, out)
		}
	}
	if string(envelope["schema"]) != "2" || string(envelope["data"]) != "[]" {
		t.Fatal(out)
	}
	after, err := os.ReadFile(filepath.Join(os.Getenv("DRIGA_DATA_DIR"), "driga.db"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("CLI wrote driga.db", err)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("DRIGA_DATA_DIR"), "cleanup.log")); !os.IsNotExist(err) {
		t.Fatal("list wrote audit log", err)
	}
}
func TestCleanupApplyNeedsYes(t *testing.T) {
	readEnv(t)
	var out bytes.Buffer
	g := &Globals{Stdin: strings.NewReader(""), Stdout: &out}
	err := (CleanupRemoveCmd{ID: "wt:test"}).runCandidates(context.Background(), g, cleanup.Env{}, []cleanup.Candidate{{ID: "wt:test", Kind: cleanup.Worktree, Status: "ready"}})
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatal(err, out.String())
	}
}
func TestCleanupAllSkipsNeedsReview(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix path or action contract; Windows phase 1 has dedicated watch/refusal tests")
	}

	readEnv(t)
	var out bytes.Buffer
	g := &Globals{Stdin: strings.NewReader(""), Stdout: &out}
	dataDir := t.TempDir()
	err := (CleanupRemoveCmd{All: true, Kind: "worktree", Yes: true}).runCandidates(context.Background(), g, cleanup.Env{DataDir: dataDir}, []cleanup.Candidate{{ID: "wt:review", Kind: cleanup.Worktree, Status: "needs_review", Ignored: []cleanup.IgnoredPath{{Path: ".env"}}}, {ID: "wt:changed", Kind: cleanup.Worktree, Status: "ready"}})
	if err == nil || !strings.Contains(out.String(), "wt:review: skipped") || !strings.Contains(out.String(), "wt:changed: failed") || !strings.Contains(out.String(), "completed 0; skipped 1; failed 1") {
		t.Fatal(err, out.String())
	}
	log, err := os.ReadFile(filepath.Join(dataDir, "cleanup.log"))
	if err != nil || bytes.Contains(log, []byte("wt:review")) || !bytes.Contains(log, []byte("wt:changed")) {
		t.Fatal("bulk selection applied review candidate", string(log), err)
	}
}
func TestCleanupNoDatabaseDoesNotCollect(t *testing.T) {
	readEnv(t)
	cleanupFake(t)
	out, _ := readRun(t, "cleanup", "--json")
	if !strings.Contains(out, `"mode":"snapshot"`) {
		t.Fatal(out)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("DRIGA_DATA_DIR"), "driga.db")); !os.IsNotExist(err) {
		t.Fatal("created persistent DB", err)
	}
}

func TestCleanupActionJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix path or action contract; Windows phase 1 has dedicated watch/refusal tests")
	}

	readEnv(t)
	cleanupFake(t)
	readDB(t, nil)
	var out, errout bytes.Buffer
	if code := run([]string{"cleanup", "remove", "--kind", "worktree", "--all", "--yes", "--json"}, "test", strings.NewReader(""), &out, &errout); code != 0 {
		t.Fatal(code, errout.String())
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil || string(envelope["schema"]) != "2" {
		t.Fatal(out.String(), err)
	}
}

func TestCleanupJSONKeepsPerTargetOutcomes(t *testing.T) {
	readEnv(t)
	c := CleanupRemoveCmd{All: true, Yes: true}
	outcomes, err := c.jsonCandidates(context.Background(), cleanup.Env{DataDir: t.TempDir()}, []cleanup.Candidate{
		{ID: "review", Kind: cleanup.Worktree, Status: "needs_review"},
		{ID: "changed", Kind: cleanup.Worktree, Status: "ready"},
	})
	if err == nil || len(outcomes) != 2 || outcomes[0].Status != "unchanged" || outcomes[1].Status != "failed" {
		t.Fatalf("%+v %v", outcomes, err)
	}
	outcomes, err = (CleanupRemoveCmd{All: true}).jsonCandidates(context.Background(), cleanup.Env{}, []cleanup.Candidate{{ID: "ready", Status: "ready"}})
	if err == nil || len(outcomes) != 1 || outcomes[0].Status != "cancelled" {
		t.Fatalf("%+v %v", outcomes, err)
	}
}

func TestCleanupConfigShown(t *testing.T) {
	readEnv(t)
	out, _ := readRun(t, "config", "show")
	for _, s := range []string{"[cleanup]", "worktree_idle = '168h'", "regenerable ="} {
		if !strings.Contains(out, s) {
			t.Fatal("missing", s, out)
		}
	}
	if err := os.WriteFile(config.Path(), []byte(out), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.Path())
	if err != nil || len(cfg.Cleanup.Regenerable) != len(config.Default().Cleanup.Regenerable) {
		t.Fatal(cfg.Cleanup, err)
	}
}
func TestCleanupWhyListingOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix path or action contract; Windows phase 1 has dedicated watch/refusal tests")
	}

	readEnv(t)
	cleanupFake(t)
	readDB(t, nil)
	readRun(t, "cleanup", "--why")
	var out, errout bytes.Buffer
	if code := run([]string{"cleanup", "remove", "wt:test", "--why"}, "test", strings.NewReader(""), &out, &errout); code == 0 || !strings.Contains(errout.String(), "unknown flag --why") {
		t.Fatal(code, errout.String())
	}
}

func TestCleanupJSONFailureIsOneRecord(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix path or action contract; Windows phase 1 has dedicated watch/refusal tests")
	}

	readEnv(t)
	cleanupFake(t)
	readDB(t, nil)
	var out, diagnostic bytes.Buffer
	code := run([]string{"cleanup", "remove", "stale-id", "--yes", "--json"}, "test", nil, &out, &diagnostic)
	if code != 1 || !json.Valid(out.Bytes()) || !strings.Contains(out.String(), `"outcomes":[]`) || !strings.Contains(out.String(), `"cleanup_failed"`) {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, &out, &diagnostic)
	}
}
