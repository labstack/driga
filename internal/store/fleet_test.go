package store

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func seedFleet(t *testing.T, s *Store) {
	t.Helper()
	mustTx(t, s, func(tx *Tx) error {
		for _, id := range []string{s.MachineID(), "B"} {
			m := tx.ForMachine(id)
			r, err := m.UpsertRepo(model.Repo{MachineID: "ignored", Path: "/r", Name: id})
			if err != nil {
				return err
			}
			w, err := m.UpsertWorktree(model.Worktree{RepoID: r, Path: "/r"})
			if err != nil {
				return err
			}
			sid, err := m.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "test", ExternalID: "same"}, RepoID: &r, WorktreeID: &w})
			if err != nil {
				return err
			}
			if err := m.UpsertInboxItem(model.InboxItem{ID: "same", SessionID: sid}); err != nil {
				return err
			}
			if err := m.ReplaceProcs([]model.Proc{{PID: 7}}); err != nil {
				return err
			}
			if err := m.ReplaceOrphans([]model.Orphan{{ID: "same"}}); err != nil {
				return err
			}
			if err := m.InsertSample(model.MachineSample{TS: time.Unix(10, 0)}); err != nil {
				return err
			}
		}
		return nil
	})
}

func TestSamePathTwoMachines(t *testing.T) {
	s := openTest(t, "", Memory)
	seedFleet(t, s)
	repos, err := s.ReposScoped(Scope{})
	if err != nil || len(repos) != 2 || repos[0].ID == repos[1].ID {
		t.Fatalf("repos: %+v %v", repos, err)
	}
	sessions, err := s.SessionsScoped(SessionFilter{RepoPath: "/r"}, Scope{})
	if err != nil || len(sessions) != 2 || sessions[0].ID == sessions[1].ID {
		t.Fatalf("sessions: %+v %v", sessions, err)
	}
	for _, session := range sessions {
		var machine string
		if err := s.db.QueryRow("SELECT machine_id FROM worktrees WHERE id=? AND repo_id=?", session.WorktreeID, session.RepoID).Scan(&machine); err != nil || machine != session.MachineID {
			t.Fatalf("cross-machine worktree: %+v %q %v", session, machine, err)
		}
	}
	local, err := s.Sessions(SessionFilter{RepoPath: "/r"})
	if err != nil || len(local) != 1 || local[0].MachineID != s.MachineID() {
		t.Fatalf("local project: %+v %v", local, err)
	}
}

func TestInboxPKPerMachine(t *testing.T) {
	s := openTest(t, "", Memory)
	seedFleet(t, s)
	at := time.Unix(20, 0).UTC()
	mustTx(t, s, func(tx *Tx) error { _, err := tx.AckInbox("same", at); return err })
	items, err := s.InboxScoped(time.Time{}, true, Scope{})
	if err != nil || len(items) != 2 {
		t.Fatalf("inbox: %+v %v", items, err)
	}
	for _, item := range items {
		if (item.AckedAt != nil) != (item.MachineID == s.MachineID()) {
			t.Fatalf("cross-machine ack: %+v", item)
		}
	}
	mustTx(t, s, func(tx *Tx) error {
		return tx.ForMachine("B").UpsertInboxItem(model.InboxItem{ID: "pending", AckPendingAt: &at})
	})
	items, err = s.InboxScoped(time.Time{}, false, Scope{Machines: []string{"B"}})
	if err != nil || len(items) != 2 {
		t.Fatalf("remote inbox: %+v %v", items, err)
	}
	for _, item := range items {
		if item.ID == "pending" && (item.AckPendingAt == nil || !item.AckPendingAt.Equal(at)) {
			t.Fatalf("pending ack: %+v", item)
		}
	}
}

func TestScopedReads(t *testing.T) {
	s := openTest(t, "", Memory)
	seedFleet(t, s)
	for _, scope := range []Scope{{}, {Machines: []string{s.MachineID()}}, {Machines: []string{}}, {Machines: []string{"missing"}}, {Machines: []string{s.MachineID(), "B"}}, {Machines: []string{"B' OR 1=1 --"}}} {
		want := 0
		if scope.Machines == nil {
			want = 2
		} else {
			for _, id := range scope.Machines {
				if id == s.MachineID() || id == "B" {
					want++
				}
			}
		}
		t.Run(fmt.Sprint(scope.Machines), func(t *testing.T) {
			sessions, err := s.SessionsScoped(SessionFilter{}, scope)
			if err != nil || len(sessions) != want {
				t.Fatalf("sessions: %+v %v", sessions, err)
			}
			repos, err := s.ReposScoped(scope)
			if err != nil || len(repos) != want {
				t.Fatalf("repos: %+v %v", repos, err)
			}
			inbox, err := s.InboxScoped(time.Time{}, false, scope)
			if err != nil || len(inbox) != want {
				t.Fatalf("inbox: %+v %v", inbox, err)
			}
			orphans, err := s.OrphansScoped(scope)
			if err != nil || len(orphans) != want {
				t.Fatalf("orphans: %+v %v", orphans, err)
			}
			procs, err := s.ProcsScoped(scope)
			if err != nil || len(procs) != want {
				t.Fatalf("procs: %+v %v", procs, err)
			}
			for _, rows := range []any{sessions, repos, inbox, orphans, procs} {
				v := reflect.ValueOf(rows)
				for i := 0; i < v.Len(); i++ {
					id := v.Index(i).FieldByName("MachineID").String()
					if id != s.MachineID() && id != "B" {
						t.Fatalf("missing identity: %+v", rows)
					}
					if scope.Machines != nil {
						selected := false
						for _, machine := range scope.Machines {
							selected = selected || machine == id
						}
						if !selected {
							t.Fatalf("row outside scope: %+v", rows)
						}
					}
				}
			}
		})
	}
	for _, read := range []func() (int, error){
		func() (int, error) { v, e := s.Repos(); return len(v), e },
		func() (int, error) { v, e := s.Sessions(SessionFilter{}); return len(v), e },
		func() (int, error) { v, e := s.Inbox(time.Time{}, true); return len(v), e },
		func() (int, error) { v, e := s.Orphans(); return len(v), e },
		func() (int, error) { v, e := s.Procs(); return len(v), e },
	} {
		if n, err := read(); err != nil || n != 1 {
			t.Fatalf("local read: %d %v", n, err)
		}
	}
	local, err := s.LatestSample()
	if err != nil || local.MachineID != s.MachineID() {
		t.Fatalf("local sample: %+v %v", local, err)
	}
	remote, err := s.LatestSampleFor("B")
	if err != nil || remote.MachineID != "B" {
		t.Fatalf("remote sample: %+v %v", remote, err)
	}
}

func TestMachinesRoundTrip(t *testing.T) {
	s := openTest(t, "", Memory)
	at := time.Unix(100, 123).UTC()
	want := model.Machine{ID: "B", MachineID: "B", Hostname: "vm", Name: "vm-1", DeviceID: "device", Addr: "127.0.0.1:7411", DrigaVersion: "dev", FeedEpoch: "epoch", FeedSeq: 42, LastContactAt: at, LastError: "offline", ClockSkewMS: 300000, State: "stale", OS: "linux", Arch: "arm64", Cores: 4, MemTotal: 1024, FirstSeen: at, LastSeen: at}
	mustTx(t, s, func(tx *Tx) error { return tx.ForMachine("B").UpsertMachine(want) })
	got, err := s.Machines()
	equal(t, []model.Machine{want}, got, err)
	want.FeedSeq++
	mustTx(t, s, func(tx *Tx) error { return tx.ForMachine("B").UpsertMachine(want) })
	got, err = s.Machines()
	equal(t, []model.Machine{want}, got, err)
	if _, err := s.Machine(); err != ErrNotFound {
		t.Fatalf("remote returned as local: %v", err)
	}
}

func TestMachineSamplePSIRoundTrip(t *testing.T) {
	s := openTest(t, "", Memory)
	at := time.Unix(100, 0).UTC()
	psi := 1.23
	for _, value := range []*float64{&psi, nil} {
		want := model.MachineSample{MachineID: "B", TS: at, PsiMemSome10: value}
		mustTx(t, s, func(tx *Tx) error { return tx.ForMachine("B").InsertSample(want) })
		got, err := s.LatestSampleFor("B")
		equal(t, want, got, err)
	}
}
