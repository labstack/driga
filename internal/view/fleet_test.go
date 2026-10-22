package view

import (
	"context"
	"encoding/json"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"strings"
	"testing"
	"time"
)

func TestPeerRecordedPathStyles(t *testing.T) {
	for _, tc := range []struct{ cwd, file, key, fileKey string }{
		{"/Work/Repo", "/Work/Repo/A.go", "/Work/Repo", "/Work/Repo/A.go"},
		{`D:\Work\Repo`, `D:\Work\Repo\A.go`, "d:/work/repo", "d:/work/repo/a.go"},
		{`\\Server\Share\Repo`, `\\Server\Share\Repo\A.go`, "//server/share/repo", "//server/share/repo/a.go"},
	} {
		t.Run(tc.cwd, func(t *testing.T) {
			st := fixture(t, func(tx *store.Tx) error {
				peer := tx.ForMachine("peer")
				if err := peer.UpsertMachine(model.Machine{OS: "linux", Hostname: "peer", State: "live"}); err != nil {
					return err
				}
				for _, id := range []string{"one", "two"} {
					sid, err := peer.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: id, Cwd: tc.cwd, LastActivityAt: testNow}, Label: "working"})
					if err != nil {
						return err
					}
					if err := peer.ReplaceSessionFiles(sid, []model.FileTouch{{Path: tc.file, TS: testNow}}); err != nil {
						return err
					}
				}
				return nil
			})
			ss, err := st.SessionsScoped(store.SessionFilter{}, store.Scope{Machines: []string{"peer"}})
			if err != nil || len(ss) != 2 || ss[0].Cwd != tc.cwd || ss[1].Cwd != tc.cwd {
				t.Fatal("stored peer path changed", ss, err)
			}
			v, err := Conflicts(st, Filter{}, testNow)
			if err != nil || len(v.Current) != 2 {
				t.Fatal("peer conflicts", v, err)
			}
			for _, c := range v.Current {
				want := tc.key
				if c.Kind == "same_file" {
					want = tc.fileKey
				}
				if c.MachineID != "peer" || c.Path != want {
					t.Fatalf("foreign conflict path: %+v, want %q", c, want)
				}
			}
		})
	}
}

func fleetFixture(t *testing.T) *store.Store {
	return fixture(t, func(tx *store.Tx) error {
		for _, id := range []string{tx.MachineID(), "remote"} {
			w := tx.ForMachine(id)
			state, name := "local", "laptop"
			if id == "remote" {
				state, name = "stale", "vm"
			}
			if err := w.UpsertMachine(model.Machine{OS: "linux", Name: name, Hostname: name, State: state, LastContactAt: testNow.Add(-12 * time.Minute), ClockSkewMS: 61000}); err != nil {
				return err
			}
			repo, err := w.UpsertRepo(model.Repo{Name: "api", Path: "/code/api"})
			if err != nil {
				return err
			}
			if _, err = w.UpsertWorktree(model.Worktree{RepoID: repo, Path: "/code/api"}); err != nil {
				return err
			}
			sid, err := w.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: "same", PID: 42, Cwd: "/code/api", StartedAt: testNow.Add(-time.Hour), LastActivityAt: testNow}, RepoID: &repo, Label: "working"})
			if err != nil {
				return err
			}
			if err = w.ReplaceSessionFiles(sid, []model.FileTouch{{Path: "/code/api/main.go", TS: testNow}}); err != nil {
				return err
			}
			if err = w.ReplaceProcs([]model.Proc{{PID: 42, Agent: "test", RSS: 100}}); err != nil {
				return err
			}
			if err = w.UpsertInboxItem(model.InboxItem{ID: "same", SessionID: sid, TS: testNow}); err != nil {
				return err
			}
			if err = w.ReplaceOrphans([]model.Orphan{{ID: "same", RootPID: 42, Cwd: "/code/api"}}); err != nil {
				return err
			}
			if err = w.InsertSample(model.MachineSample{TS: testNow, MemUsed: 100}); err != nil {
				return err
			}
		}
		return nil
	})
}

func TestViewsMergeByDefault(t *testing.T) {
	s := fleetFixture(t)
	a, e := Agents(s, Filter{}, testNow)
	if e != nil || len(a.Rows) != 2 {
		t.Fatalf("agents %+v %v", a, e)
	}
	p, e := Projects(s, Filter{}, testNow)
	if e != nil || len(p.Rows) != 2 {
		t.Fatalf("projects %+v %v", p, e)
	}
	i, e := Inbox(s, Filter{}, testNow)
	if e != nil || len(i.Rows) != 2 {
		t.Fatalf("inbox %+v %v", i, e)
	}
	o, e := Orphans(s, Filter{}, testNow)
	if e != nil || len(o.Rows) != 2 {
		t.Fatalf("orphans %+v %v", o, e)
	}
	m, e := Machine(s, Filter{}, testNow)
	if e != nil || len(m.Memory) != 2 {
		t.Fatalf("machine %+v %v", m, e)
	}
	sum, e := Summary(s, Filter{}, testNow)
	if e != nil || sum.Agents != 2 || sum.Inbox != 2 || sum.Orphans != 2 {
		t.Fatalf("summary %+v %v", sum, e)
	}
	// Identical PIDs must not inflate either agent's memory.
	for _, r := range a.Rows {
		if r.Mem != 100 {
			t.Fatalf("cross-machine processes: %+v", r)
		}
	}
}
func TestMachinesView(t *testing.T) {
	s := fleetFixture(t)
	v, e := Machines(s, testNow)
	if e != nil || len(v.Rows) != 2 {
		t.Fatalf("%+v %v", v, e)
	}
	for _, r := range v.Rows {
		if r.MachineID == "remote" && (r.State != "stale" || r.Agents != 1 || r.Inbox != 1 || r.SkewWarning == "" || r.LastSeenAge != 12*time.Minute) {
			t.Fatalf("%+v", r)
		}
	}
}
func TestConflictsNeverCrossMachines(t *testing.T) {
	s := fleetFixture(t)
	v, e := Conflicts(s, Filter{}, testNow)
	if e != nil || len(v.Current)+len(v.Recent) != 0 {
		t.Fatalf("cross-machine conflicts %+v %v", v, e)
	}
}
func TestJSONRowsCarryMachineID(t *testing.T) {
	s := fleetFixture(t)
	a, _ := Agents(s, Filter{}, testNow)
	p, _ := Projects(s, Filter{}, testNow)
	i, _ := Inbox(s, Filter{}, testNow)
	o, _ := Orphans(s, Filter{}, testNow)
	for _, v := range []any{a, p, i, o} {
		b, e := json.Marshal(v)
		if e != nil || !strings.Contains(string(b), `"machine_id":"remote"`) {
			t.Fatalf("%s %v", b, e)
		}
	}
	ss, _ := s.SessionsScoped(store.SessionFilter{}, store.Scope{Machines: []string{"remote"}})
	d, e := Agent(s, ss[0].ID, testNow)
	if e != nil || d.MachineID != "remote" || len(d.Procs) != 1 || len(d.Files) != 1 {
		t.Fatalf("detail %+v %v", d, e)
	}
}

func TestConflictsRetainMachineLocalPairs(t *testing.T) {
	s := fleetFixture(t)
	if e := s.Tx(context.Background(), func(tx *store.Tx) error {
		for _, machine := range []string{tx.MachineID(), "remote"} {
			w := tx.ForMachine(machine)
			id, e := w.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: "second", Cwd: "/code/api", LastActivityAt: testNow}, Label: "working"})
			if e != nil {
				return e
			}
			if e = w.ReplaceSessionFiles(id, []model.FileTouch{{Path: "/code/api/main.go", TS: testNow}}); e != nil {
				return e
			}
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	v, e := Conflicts(s, Filter{}, testNow)
	if e != nil {
		t.Fatal(e)
	}
	if len(v.Current) != 4 {
		t.Fatalf("%+v", v)
	}
	for _, c := range v.Current {
		if len(c.SessionIDs) != 2 {
			t.Fatalf("cross-machine pair: %+v", c)
		}
		for _, id := range c.SessionIDs {
			row, e := s.ReadMachine(c.MachineID).Session(id)
			if e != nil || row.MachineID != c.MachineID {
				t.Fatalf("cross-machine pair: %+v %v", c, e)
			}
		}
	}
}
