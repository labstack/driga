package collector

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/adapter"
	"github.com/labstack/driga/internal/adapter/claude"
	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/fixturegen"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestClaudeRegistrySwitchOwnsProcess(t *testing.T) {
	d, st, p, _ := setup(t)
	home := os.Getenv("HOME")
	if err := fixturegen.Write(home, fixturegen.Scenario{Now: d.Now(), Repos: 1, Agents: []fixturegen.AgentSpec{{Adapter: "claude", ID: "before-clear", Status: "idle"}}}); err != nil {
		t.Fatal(err)
	}

	materializeFixture(t, home)
	registry := filepath.Join(home, ".claude", "sessions", "4000.json")
	b, err := os.ReadFile(registry)
	if err != nil {
		t.Fatal(err)
	}
	var entry map[string]any
	if err := json.Unmarshal(b, &entry); err != nil {
		t.Fatal(err)
	}
	start, _ := time.Parse(time.RFC3339Nano, entry["startedAt"].(string))
	p.procs = []model.Proc{{PID: 4000, StartTime: start, Cmdline: "claude", Argv: []string{"claude"}, RSS: 100, CPUSeconds: 1}}
	d.Adapters = []adapter.Adapter{claude.New()}
	pass(t, d, st, Plan{Procs: true})
	old := sessions(t, st)[0]
	// Persist the hook-first shape seen before a registry adopts the session.
	old.Hosted, old.TurnOpen = true, true
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		_, err := tx.UpsertSession(old)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(home, ".claude", "projects", "heldout-grove", "before-clear.jsonl")
	b, err = os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(log), "after-clear.jsonl"), b, 0600); err != nil {
		t.Fatal(err)
	}
	entry["sessionId"], entry["statusUpdatedAt"] = "after-clear", d.Now().Format(time.RFC3339Nano)
	b, _ = json.Marshal(entry)
	if err := os.WriteFile(registry, b, 0600); err != nil {
		t.Fatal(err)
	}
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		return tx.InsertEvent(model.Event{Adapter: "claude", ExternalID: "after-clear", Kind: "Stop", TS: d.Now().Add(-10 * time.Second)})
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		ended := d.Now().Add(-time.Hour)
		id, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "after-clear", Hosted: true}, EndedAt: &ended})
		if err != nil {
			return err
		}
		return tx.UpsertInboxItem(model.InboxItem{ID: "stop", SessionID: id, Kind: "finished", TS: d.Now().Add(-10 * time.Second)})
	}); err != nil {
		t.Fatal(err)
	}
	for range 2 { // Detachment must survive transcript-only deltas and restart.
		d.State = NewState()
		pass(t, d, st, Plan{Procs: true})
		var current model.Session
		for _, s := range sessions(t, st) {
			if s.ExternalID == "after-clear" {
				current = s
			} else if s.PID != 0 || s.Hosted || s.EndedAt == nil || s.Label != "ended" {
				t.Errorf("previous conversation still live: %+v", s)
			}
		}
		if current.PID != 4000 || current.Hosted || current.EndedAt != nil || current.Label != "idle" {
			t.Fatalf("registry current session: %+v", current)
		}
		live, err := st.Sessions(store.SessionFilter{})
		if err != nil || len(live) != 1 || live[0].ID != current.ID {
			t.Fatalf("live inventory: %+v %v", live, err)
		}
		ps, err := st.Procs()
		if err != nil {
			t.Fatal(err)
		}
		placement := derive.PlaceProcesses(ps, sessions(t, st), nil)[4000]
		if placement.Attribution == nil || placement.Attribution.SessionID != current.ID || placement.Memory != 100 {
			t.Fatalf("machine attribution: %+v", placement)
		}
	}
}

func materializeFixture(t *testing.T, home string) {
	t.Helper()
	var markers []string
	err := filepath.WalkDir(home, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Name() == "git.fixture" {
			markers = append(markers, path)
		}
		if entry.IsDir() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), fixturegen.HomeToken) {
			b = []byte(strings.ReplaceAll(string(b), fixturegen.HomeToken, filepath.ToSlash(home)))
			return os.WriteFile(path, b, 0600)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := len(markers) - 1; i >= 0; i-- {
		if err := os.Rename(markers[i], filepath.Join(filepath.Dir(markers[i]), ".git")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestClaudeRegistryPIDReuseDoesNotBecomeHosted(t *testing.T) {
	d, st, p, a := setup(t)
	old := a.obs[0]
	old.Hosted = true
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		_, err := tx.UpsertSession(model.Session{SessionObs: old})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	p.procs[0].StartTime = d.Now()
	a.obs[0].ExternalID, a.obs[0].ProcStart = "new-process", d.Now()
	pass(t, d, st, Plan{Procs: true})
	for _, s := range sessions(t, st) {
		if s.ExternalID == old.ExternalID && (s.Hosted || s.PID != 0 || s.EndedAt == nil) {
			t.Fatalf("dead registry binding became a hosted session: %+v", s)
		}
	}
}
