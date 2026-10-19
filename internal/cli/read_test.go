package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/gitscan"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func readEnv(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("DRIGA_DATA_DIR", filepath.Join(home, "data"))
	t.Setenv("DRIGA_SPOOL_DIR", filepath.Join(home, "spool"))
	t.Setenv("DRIGA_CONFIG", filepath.Join(home, "config.toml"))
}

func readDB(t *testing.T, seed func(*store.Tx) error) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(config.DataDir(), "driga.db"), store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.SetMeta("last_pass_at", time.Now().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	// Reads trust the database only while its collector is alive; this test
	// process stands in for it.
	if err := os.WriteFile(filepath.Join(config.DataDir(), "driga.lock"), []byte(strconv.Itoa(os.Getpid())+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if seed != nil {
		if err := s.Tx(context.Background(), seed); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func readRun(t *testing.T, args ...string) (string, string) {
	t.Helper()
	var out, errout bytes.Buffer
	if code := run(args, "test", strings.NewReader(""), &out, &errout); code != 0 {
		t.Fatalf("%v: code=%d stderr=%s", args, code, errout.String())
	}
	return out.String(), errout.String()
}

func TestProjectFilterDotPathName(t *testing.T) {
	readEnv(t)
	path := t.TempDir()
	t.Chdir(path)
	path = gitscan.NormalizePath(path)
	readDB(t, func(tx *store.Tx) error {
		repo, err := tx.UpsertRepo(model.Repo{Name: "fixture", Path: path})
		if err != nil {
			return err
		}
		if _, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: "selected", LastActivityAt: time.Now()}, RepoID: &repo}); err != nil {
			return err
		}
		_, err = tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: "other", LastActivityAt: time.Now()}})
		return err
	})
	var first string
	for _, p := range []string{".", path, "fixture"} {
		out, _ := readRun(t, "agents", "--project", p, "--json")
		var v struct{ Data json.RawMessage }
		if err := json.Unmarshal([]byte(out), &v); err != nil {
			t.Fatal(err)
		}
		// Ages can vary; IDs and row count must agree.
		var data struct{ Rows []struct{ ID int64 } }
		if err := json.Unmarshal(v.Data, &data); err != nil {
			t.Fatal(err)
		}
		if len(data.Rows) != 1 {
			t.Fatal(out)
		}
		key := fmt.Sprint(data.Rows[0].ID)
		if first != "" && key != first {
			t.Fatal(out)
		}
		first = key
	}
}

func TestSinceYesterday(t *testing.T) {
	zone := time.FixedZone("local", -7*60*60)
	now := time.Date(2026, 10, 18, 13, 45, 0, 0, zone)
	want := time.Date(2026, 10, 17, 0, 0, 0, 0, zone)
	got, err := parseSince("yesterday", now)
	if err != nil || !got.Equal(want) {
		t.Fatalf("%v %v", got, err)
	}
	got, err = parseSince("2h", now)
	if err != nil || !got.Equal(now.Add(-2*time.Hour)) {
		t.Fatalf("%v %v", got, err)
	}
	if _, err := parseSince("-1h", now); err == nil {
		t.Fatal("negative duration accepted")
	}
	readEnv(t)
	readDB(t, func(tx *store.Tx) error {
		for _, item := range []model.InboxItem{{ID: "new", TS: time.Now()}, {ID: "old", TS: time.Now().Add(-72 * time.Hour)}} {
			if err := tx.UpsertInboxItem(item); err != nil {
				return err
			}
		}
		return nil
	})
	out, _ := readRun(t, "inbox", "--since", "yesterday", "--json")
	if strings.Contains(out, `"old"`) || !strings.Contains(out, `"new"`) {
		t.Fatal(out)
	}
}

func TestReadFallsBackToSnapshot(t *testing.T) {
	readEnv(t)
	for i := 0; i < 2; i++ {
		out, errout := readRun(t, "--json")
		var e struct{ Mode string }
		if err := json.Unmarshal([]byte(out), &e); err != nil {
			t.Fatal(err)
		}
		if e.Mode != "snapshot" {
			t.Fatal(out)
		}
		if strings.Contains(errout, "run 'driga service install'") != (i == 0) {
			t.Fatal(errout)
		}
	}
	if _, err := os.Stat(filepath.Join(config.DataDir(), "driga.db")); !os.IsNotExist(err) {
		t.Fatalf("snapshot created DB: %v", err)
	}
	out, _ := readRun(t)
	if !strings.Contains(out, "snapshot · no history") {
		t.Fatal(out)
	}
}

func TestReadsNeverAck(t *testing.T) {
	readEnv(t)
	var id int64
	s := readDB(t, func(tx *store.Tx) error {
		var err error
		id, err = tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "session", Cwd: "/fixture", LastActivityAt: time.Now()}, Label: "finished"})
		if err != nil {
			return err
		}
		return tx.UpsertInboxItem(model.InboxItem{ID: "completion", SessionID: id, Kind: "finished", TS: time.Now()})
	})
	readRun(t, "inbox", "--json")
	readRun(t, "agent", fmt.Sprint(id))
	items, err := s.Inbox(time.Time{}, true)
	if err != nil || len(items) != 1 || items[0].AckedAt != nil {
		t.Fatalf("%+v %v", items, err)
	}
}

func TestReadCommands(t *testing.T) {
	readEnv(t)
	readDB(t, func(tx *store.Tx) error {
		_, err := tx.UpsertRepo(model.Repo{Path: "/fixture", Name: "fixture"})
		return err
	})
	for _, args := range [][]string{{}, {"summary"}, {"agents", "--all"}, {"projects", "--active"}, {"projects", "--at-risk"}, {"where", "fixture"}, {"inbox"}, {"inbox", "--all"}, {"inbox", "list", "--all"}, {"conflicts"}, {"orphans"}, {"orphans", "list"}, {"machine"}} {
		readRun(t, args...)
		readRun(t, append(append([]string{}, args...), "--json")...)
	}
}

func TestReadFallbackPolicies(t *testing.T) {
	for _, kind := range []string{"fresh", "missing-meta", "bad-meta", "stale", "unreadable", "forced"} {
		t.Run(kind, func(t *testing.T) {
			readEnv(t)
			if err := os.WriteFile(config.Path(), []byte("no_service_hint = true\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if kind == "unreadable" {
				if err := os.MkdirAll(config.DataDir(), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(config.DataDir(), "driga.db"), []byte("invalid SQLite"), 0600); err != nil {
					t.Fatal(err)
				}
			} else if kind == "missing-meta" {
				s, err := store.Open(filepath.Join(config.DataDir(), "driga.db"), store.ReadWrite)
				if err != nil {
					t.Fatal(err)
				}
				s.Close()
			} else {
				s := readDB(t, nil)
				value := time.Now().Format(time.RFC3339Nano)
				if kind == "stale" {
					value = time.Now().Add(-11 * time.Minute).Format(time.RFC3339Nano)
				}
				if kind == "bad-meta" {
					value = "not a date"
				}
				if err := s.SetMeta("last_pass_at", value); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(filepath.Join(config.DataDir(), "driga.db"))
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"machine", "--json"}
			if kind == "forced" {
				args = append(args, "--snapshot")
			}
			out, errout := readRun(t, args...)
			var envelope struct{ Mode string }
			if err := json.Unmarshal([]byte(out), &envelope); err != nil {
				t.Fatal(err)
			}
			want := "snapshot"
			if kind == "fresh" {
				want = "daemon"
			}
			if envelope.Mode != want || errout != "" {
				t.Fatalf("mode=%s stderr=%s", envelope.Mode, errout)
			}
			after, err := os.ReadFile(filepath.Join(config.DataDir(), "driga.db"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("read changed the persistent DB")
			}
			if _, err := os.Stat(filepath.Join(config.DataDir(), "hint")); !os.IsNotExist(err) {
				t.Fatalf("disabled hint wrote state: %v", err)
			}
		})
	}
}

func TestReadFreshnessAndCoverage(t *testing.T) {
	readEnv(t)
	now := time.Now()
	readDB(t, func(tx *store.Tx) error {
		if err := tx.SetSourceOK("procs", now.Add(-30*time.Second)); err != nil {
			return err
		}
		if err := tx.SetSourceOK("git", now.Add(-10*time.Minute)); err != nil {
			return err
		}
		for _, gap := range []model.Gap{{From: now.Add(-3 * time.Hour), To: now.Add(-2 * time.Hour)}, {From: now.Add(-30 * time.Minute), To: now.Add(-20 * time.Minute)}} {
			if err := tx.InsertGap(gap); err != nil {
				return err
			}
		}
		return nil
	})
	for _, active := range []bool{false, true} {
		if active {
			if err := os.WriteFile(filepath.Join(config.DataDir(), "viewer.heartbeat"), nil, 0600); err != nil {
				t.Fatal(err)
			}
		}
		out, _ := readRun(t, "agents", "--since", "1h", "--json")
		var e struct {
			Freshness    map[string]struct{ Stale bool }
			CoverageGaps []model.Gap `json:"coverage_gaps"`
		}
		if err := json.Unmarshal([]byte(out), &e); err != nil {
			t.Fatal(err)
		}
		if e.Freshness["procs"].Stale != active || e.Freshness["git"].Stale || len(e.CoverageGaps) != 1 {
			t.Fatal(out)
		}
	}
	out, _ := readRun(t, "agents")
	if !strings.Contains(out, "stale: procs") || !strings.Contains(out, "coverage gap:") {
		t.Fatal(out)
	}
}

func TestReadProjectErrors(t *testing.T) {
	readEnv(t)
	readDB(t, func(tx *store.Tx) error {
		for _, path := range []string{"/first/repo", "/second/repo"} {
			if _, err := tx.UpsertRepo(model.Repo{Name: "duplicate", Path: path}); err != nil {
				return err
			}
		}
		return nil
	})
	for _, tc := range []struct{ project, want string }{{"duplicate", "ambiguous"}, {"missing", "unknown project"}, {"/first/repository", "unknown project path"}} {
		var out, errout bytes.Buffer
		if code := run([]string{"agents", "--json", "--project", tc.project}, "test", strings.NewReader(""), &out, &errout); code == 0 || !strings.Contains(errout.String(), tc.want) {
			t.Fatalf("%s: code=%d %s", tc.project, code, errout.String())
		}
	}
}

// A recent database without a live collector is not presented as daemon
// data: the read collects a fresh snapshot instead.
func TestReadWithoutLiveCollectorIsSnapshot(t *testing.T) {
	readEnv(t)
	readDB(t, nil)
	if err := os.Remove(filepath.Join(config.DataDir(), "driga.lock")); err != nil {
		t.Fatal(err)
	}
	out, _ := readRun(t, "agents", "--json")
	if !strings.Contains(out, `"mode": "snapshot"`) && !strings.Contains(out, `"mode":"snapshot"`) {
		t.Fatalf("mode: %s", out[:min(len(out), 200)])
	}
}
