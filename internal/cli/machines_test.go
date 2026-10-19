package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/fleet"
	"github.com/labstack/driga/internal/gitscan"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func machineDB(t *testing.T, path string) *store.Store {
	return readDB(t, func(tx *store.Tx) error {
		for _, id := range []string{tx.MachineID(), "remote-id"} {
			w := tx.ForMachine(id)
			name := "laptop"
			if id == "remote-id" {
				name = "vm"
			}
			if e := w.UpsertMachine(model.Machine{Name: name, Hostname: name, DeviceID: "DEVICE-" + id, State: "live", LastContactAt: time.Now().Add(-12 * time.Minute), LastError: "connection lost"}); e != nil {
				return e
			}
			rid, e := w.UpsertRepo(model.Repo{Name: "api", Path: path})
			if e != nil {
				return e
			}
			if _, e = w.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: id, LastActivityAt: time.Now()}, RepoID: &rid}); e != nil {
				return e
			}
			if e = w.ReplaceOrphans([]model.Orphan{{ID: "remote-orphan", Verified: true}}); e != nil {
				return e
			}
		}
		return nil
	})
}
func TestMachineFlagScopes(t *testing.T) {
	readEnv(t)
	s := machineDB(t, "/code/api")
	for _, tc := range []struct {
		flag string
		n    int
		id   string
	}{{"all", 2, ""}, {"local", 1, s.MachineID()}, {"vm", 1, "remote-id"}, {"remote-id", 1, "remote-id"}, {"DEVICE-remote", 1, "remote-id"}} {
		out, _ := readRun(t, "agents", "--machine", tc.flag, "--json")
		var e struct {
			Data struct {
				Rows []struct {
					MachineID string `json:"machine_id"`
				}
			}
		}
		if err := json.Unmarshal([]byte(out), &e); err != nil {
			t.Fatal(err)
		}
		if len(e.Data.Rows) != tc.n || tc.id != "" && e.Data.Rows[0].MachineID != tc.id {
			t.Fatal(out)
		}
	}
	var out, errs bytes.Buffer
	if run([]string{"agents", "--machine", "unknown"}, "test", strings.NewReader(""), &out, &errs) == 0 || !strings.Contains(errs.String(), "vm") || !strings.Contains(errs.String(), "laptop") {
		t.Fatalf("%s %s", out.String(), errs.String())
	}
}
func TestProjectDotIsLocal(t *testing.T) {
	readEnv(t)
	path := t.TempDir()
	t.Chdir(path)
	s := machineDB(t, gitscan.NormalizePath(path))
	out, _ := readRun(t, "agents", "--project", ".", "--machine", "all", "--json")
	if strings.Contains(out, `"machine_id": "remote-id"`) || !strings.Contains(out, s.MachineID()) {
		t.Fatal(out)
	}
}
func TestKillRefusesRemoteOrphan(t *testing.T) {
	readEnv(t)
	s := machineDB(t, "/code/api")
	if e := s.Tx(context.Background(), func(tx *store.Tx) error { return tx.ReplaceOrphans(nil) }); e != nil {
		t.Fatal(e)
	}
	var out, errs bytes.Buffer
	code := run([]string{"orphans", "kill", "remote-orphan"}, "test", strings.NewReader("y\n"), &out, &errs)
	if code == 0 || !strings.Contains(errs.String(), "orphan is on vm; run driga orphans kill there") || strings.Contains(out.String(), "Kill ") {
		t.Fatalf("%d %s %s", code, out.String(), errs.String())
	}
}
func TestFleetPeersShowsStateAndLastSeen(t *testing.T) {
	readEnv(t)
	machineDB(t, "/code/api")
	if e := fleet.SavePeers(filepath.Join(config.DataDir(), "fleet"), []fleet.Peer{{Name: "vm", MachineID: "remote-id", Addr: "127.0.0.1:7411", DeviceID: "DEVICE-remote"}}); e != nil {
		t.Fatal(e)
	}
	out, _ := readRun(t, "fleet", "peers")
	for _, want := range []string{"STATE", "LAST SEEN", "stale", "12m", "connection lost"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: %s", want, out)
		}
	}
	out, _ = readRun(t, "fleet", "peers", "--json")
	for _, want := range []string{`"state":"stale"`, `"last_seen":`, `"last_error":"connection lost"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: %s", want, out)
		}
	}
}

func TestMachinesCommandUsesCachedRows(t *testing.T) {
	readEnv(t)
	s := machineDB(t, "/code/api")
	if err := os.Remove(filepath.Join(config.DataDir(), "driga.lock")); err != nil {
		t.Fatal(err)
	}
	if e := s.SetMeta("last_pass_at", time.Now().Add(-time.Hour).Format(time.RFC3339Nano)); e != nil {
		t.Fatal(e)
	}
	out, _ := readRun(t, "machines")
	for _, want := range []string{"NAME", "vm", "stale", "viewer collector not running", "connection lost"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: %s", want, out)
		}
	}
	out, _ = readRun(t, "machines", "--json", "--machine", "vm")
	if strings.Contains(out, `"name": "laptop"`) || !strings.Contains(out, `"machine_id": "remote-id"`) {
		t.Fatal(out)
	}
}

func TestRemoteAckDoesNotSpoolLocally(t *testing.T) {
	readEnv(t)
	s := machineDB(t, "/code/api")
	if e := s.Tx(context.Background(), func(tx *store.Tx) error {
		return tx.ForMachine("remote-id").UpsertInboxItem(model.InboxItem{ID: "remote-item", TS: time.Now()})
	}); e != nil {
		t.Fatal(e)
	}
	var out, errs bytes.Buffer
	if run([]string{"inbox", "ack", "remote-item"}, "test", strings.NewReader(""), &out, &errs) == 0 {
		t.Fatalf("remote ack accepted locally: %s", out.String())
	}
	if files, _ := filepath.Glob(filepath.Join(config.SpoolDir(), "*")); len(files) != 0 {
		t.Fatal("remote ack spooled locally", files)
	}
}

func TestRemoteAckScopeDoesNotSpoolUnknownID(t *testing.T) {
	readEnv(t)
	machineDB(t, "/code/api")
	var out, errs bytes.Buffer
	if run([]string{"inbox", "ack", "missing", "--machine", "vm"}, "test", strings.NewReader(""), &out, &errs) == 0 {
		t.Fatalf("remote scoped ack became local: %s", out.String())
	}
	if files, _ := filepath.Glob(filepath.Join(config.SpoolDir(), "*")); len(files) != 0 {
		t.Fatal("remote ack spooled locally", files)
	}
}

func TestMachinesProbeDoesNotWriteLock(t *testing.T) {
	readEnv(t)
	machineDB(t, "/code/app")
	path := filepath.Join(config.DataDir(), "driga.lock")
	before := []byte("stopped collector metadata\n")
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	readRun(t, "machines")
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, before) {
		t.Fatalf("probe mutated lock: %q %v", got, err)
	}
}
