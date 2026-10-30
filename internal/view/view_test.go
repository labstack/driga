package view

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

var testNow = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

func fixture(t *testing.T, seed func(*store.Tx) error) *store.Store {
	t.Helper()
	s, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if seed != nil {
		if err := s.Tx(context.Background(), func(tx *store.Tx) error {
			if err := tx.UpsertMachine(model.Machine{OS: "linux"}); err != nil {
				return err
			}
			return seed(tx)
		}); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func TestEnvelopeShape(t *testing.T) {
	s := fixture(t, nil)
	e, err := NewEnvelope(s, "daemon", testNow, collector.Cadence(time.Hour), AgentsView{})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Render(&out, AgentsView{Rows: []AgentRow{}}, RenderOpts{JSON: true, Envelope: e}); err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	keys := []string{}
	for k := range got {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	want := []string{"coverage_gaps", "data", "freshness", "mode", "observed_at", "schema"}
	if !reflect.DeepEqual(keys, want) || string(got["schema"]) != "2" || string(got["coverage_gaps"]) != "[]" {
		t.Fatal(out.String())
	}
}

func TestFreshnessStale(t *testing.T) {
	s := fixture(t, func(tx *store.Tx) error {
		for name, age := range map[string]time.Duration{"procs": 16 * time.Second, "git": 10 * time.Minute, "machine": 30 * time.Second, "custom": 15 * time.Second} {
			if err := tx.SetSourceOK(name, testNow.Add(-age)); err != nil {
				return err
			}
		}
		return tx.SetSourceError("procs", "permission denied", testNow)
	})
	for _, mode := range []string{"daemon", "snapshot"} {
		e, err := NewEnvelope(s, mode, testNow, collector.Cadence(0), nil)
		if err != nil {
			t.Fatal(err)
		}
		for name, f := range e.Freshness {
			want := mode == "daemon" && name == "procs"
			if f.Stale != want {
				t.Errorf("%s %s: %+v", mode, name, f)
			}
		}
		if e.Freshness["procs"].AgeMS != 16000 || e.Freshness["procs"].LastError != "permission denied" {
			t.Fatal(e)
		}
	}
}

func TestSummaryLineFormat(t *testing.T) {
	s := fixture(t, func(tx *store.Tx) error {
		for _, label := range []string{"working", "waiting", "stuck"} {
			if _, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: label, StartedAt: testNow, LastActivityAt: testNow}, Label: label, Heuristic: true}); err != nil {
				return err
			}
		}
		if err := tx.UpsertInboxItem(model.InboxItem{ID: "event", Kind: "finished", TS: testNow}); err != nil {
			return err
		}
		if err := tx.ReplaceOrphans([]model.Orphan{{ID: "orphan", RSSTotal: 512 << 20}}); err != nil {
			return err
		}
		return tx.InsertSample(model.MachineSample{TS: testNow, SwapUsed: 15 << 30, SwapTotal: 16 << 30, Load1: 2.4, Cores: 8})
	})
	v, err := Summary(s, Filter{}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Render(&out, v, RenderOpts{Envelope: Envelope{Mode: "daemon"}}); err != nil {
		t.Fatal(err)
	}
	want := "3 agents (1 working, 1 waiting, 1 stuck) · inbox 1 · swap 15.0G/16.0G · load 2/8 · orphans 1 ~512.0M RSS · daemon\n"
	if out.String() != want {
		t.Fatalf("got %q, want %q", out.String(), want)
	}
}

func TestAgentsGroupedByFamily(t *testing.T) {
	var parent int64
	s := fixture(t, func(tx *store.Tx) error {
		var err error
		parent, err = tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: "parent", PID: 100, StartedAt: testNow.Add(-time.Hour), LastActivityAt: testNow.Add(-time.Minute)}, Label: "working"})
		if err != nil {
			return err
		}
		child, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: "child", PID: 101, StartedAt: testNow, LastActivityAt: testNow}, ParentID: &parent, Label: "idle"})
		if err != nil {
			return err
		}
		if err := tx.UpsertInboxItem(model.InboxItem{ID: "finished", SessionID: child, Kind: "finished", TS: testNow}); err != nil {
			return err
		}
		return tx.ReplaceProcs([]model.Proc{{PID: 100, RSS: 10}, {PID: 101, PPID: 100, RSS: 20}, {PID: 102, PPID: 101, RSS: 30}})
	})
	v, err := Agents(s, AgentFilter{}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Rows) != 1 || v.Rows[0].ID != parent || len(v.Rows[0].Children) != 1 || v.Rows[0].Mem != 60 || v.Rows[0].FinishedChildren != 1 {
		t.Fatalf("%+v", v)
	}
	var out bytes.Buffer
	if err := Render(&out, v, RenderOpts{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "↳ ") || !strings.Contains(out.String(), "(1 workers finished)") {
		t.Fatal(out.String())
	}
}

func TestHiddenStaleHosted(t *testing.T) {
	s := fixture(t, func(tx *store.Tx) error {
		for _, id := range []string{"stale", "fresh", "local"} {
			at := testNow
			if id != "fresh" {
				at = at.Add(-25 * time.Hour)
			}
			if _, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: id, Hosted: id != "local", LastActivityAt: at}, Label: "idle"}); err != nil {
				return err
			}
		}
		return nil
	})
	for _, all := range []bool{false, true} {
		v, err := Agents(s, AgentFilter{All: all}, testNow)
		if err != nil {
			t.Fatal(err)
		}
		if len(v.Rows) != 2 {
			t.Fatal(v)
		}
	}
}

func TestConflictsComputedAtRead(t *testing.T) {
	s := fixture(t, func(tx *store.Tx) error {
		repo, err := tx.UpsertRepo(model.Repo{Path: "/repo", Name: "repo"})
		if err != nil {
			return err
		}
		for i, path := range []string{"/repo", "/linked"} {
			if _, err := tx.UpsertWorktree(model.Worktree{RepoID: repo, Path: path, Branch: "main"}); err != nil {
				return err
			}
			id, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: path, Cwd: path, Branch: "main"}, RepoID: &repo, Label: "working"})
			if err != nil {
				return err
			}
			if err := tx.ReplaceSessionFiles(id, []model.FileTouch{{Path: path + "/file.go", TS: testNow.Add(-time.Duration(i) * time.Minute)}}); err != nil {
				return err
			}
		}
		return nil
	})
	v, err := Conflicts(s, Filter{}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Current) != 1 || v.Current[0].Kind != "same_file_other_worktree" {
		t.Fatal(v)
	}
	v, err = Conflicts(s, Filter{}, testNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Current) != 0 || len(v.Recent) != 1 {
		t.Fatal(v)
	}
	v, err = Conflicts(s, Filter{}, testNow.Add(7*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Current)+len(v.Recent) != 0 {
		t.Fatal(v)
	}
}

func TestNoColorEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var out bytes.Buffer
	err := Render(&out, AgentsView{Rows: []AgentRow{{Label: "working", Children: []AgentRow{}}}}, RenderOpts{})
	if err != nil || strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("%v %q", err, out.String())
	}
}

func TestDetailRowsCarryMachineID(t *testing.T) {
	var id int64
	s := fixture(t, func(tx *store.Tx) error {
		var err error
		id, err = tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: "detail", PID: 100}})
		if err != nil {
			return err
		}
		if err := tx.ReplaceProcs([]model.Proc{{PID: 100}}); err != nil {
			return err
		}
		if err := tx.ReplaceSessionFiles(id, []model.FileTouch{{Path: "/file", TS: testNow}}); err != nil {
			return err
		}
		return tx.InsertEvent(model.Event{Adapter: "test", ExternalID: "detail", TS: testNow, Kind: "tool"})
	})
	v, err := Agent(s, id, testNow)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"files", "procs", "events"} {
		var rows []map[string]any
		if err := json.Unmarshal(got[key], &rows); err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0]["machine_id"] != s.MachineID() {
			t.Errorf("%s: %s", key, got[key])
		}
	}
}
