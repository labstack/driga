package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/pressly/goose/v3"
)

func TestMigration0003UpgradesV1DB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	old := fstest.MapFS{}
	for _, name := range []string{"0001_init.sql", "0002_orphan_verified.sql"} {
		b, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		old[name] = &fstest.MapFile{Data: b}
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, db, old, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Seed every v1 table, including all foreign-key dependents. Remember each
	// original column so newly added columns do not hide lost or changed values.
	tables := []string{"meta", "machines", "repos", "worktrees", "sessions", "session_files", "events", "handoffs", "procs", "orphans", "machine_samples", "offsets", "inbox_items", "sources", "gaps"}
	columns, before := map[string]string{}, map[string]string{}
	for _, table := range tables {
		rows, err := db.Query("PRAGMA table_info(" + table + ")")
		if err != nil {
			t.Fatal(err)
		}
		var cols, vals []string
		for rows.Next() {
			var cid, notnull, pk int
			var name, typ string
			var def any
			if err := rows.Scan(&cid, &name, &typ, &notnull, &def, &pk); err != nil {
				t.Fatal(err)
			}
			cols = append(cols, name)
			value := "1"
			if typ == "TEXT" {
				value = "'value'"
			}
			switch name {
			case "machine_id":
				value = "'A'"
			case "id":
				if table == "machines" {
					value = "'A'"
				}
			case "key":
				value = "'machine_id'"
			case "value":
				value = "'A'"
			case "evidence", "pids":
				value = "'[]'"
			}
			vals = append(vals, value)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		columns[table] = strings.Join(cols, ",")
		if _, err := db.Exec("INSERT INTO " + table + " (" + columns[table] + ") VALUES (" + strings.Join(vals, ",") + ")"); err != nil {
			t.Fatal(err)
		}
		var snapshot string
		if err := db.QueryRow("SELECT json_array(" + columns[table] + ") FROM " + table).Scan(&snapshot); err != nil {
			t.Fatal(err)
		}
		before[table] = snapshot
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s := openTest(t, path, ReadWrite)
	for _, table := range tables {
		var got string
		if err := s.db.QueryRow("SELECT json_array(" + columns[table] + ") FROM " + table).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != before[table] {
			t.Fatalf("%s changed: %s -> %s", table, before[table], got)
		}
	}
	var fk int
	if err := s.db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("foreign keys: %d %v", fk, err)
	}
	rows, err := s.db.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		t.Fatal("migration broke foreign keys")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	mustTx(t, s, func(tx *Tx) error {
		_, err := tx.ForMachine("B").UpsertRepo(model.Repo{Path: "value"})
		if err != nil {
			return err
		}
		return tx.ForMachine("B").UpsertInboxItem(model.InboxItem{ID: "1"})
	})
	var epoch string
	if err := s.db.QueryRow("SELECT value FROM meta WHERE key='feed_epoch'").Scan(&epoch); err != nil || len(epoch) != 32 {
		t.Fatalf("epoch: %q %v", epoch, err)
	}
	if _, err := s.db.Exec("INSERT INTO feed(ts,kind,key,payload) VALUES(0,'repo','x','{}')"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("DELETE FROM feed"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("INSERT INTO feed(ts,kind,key,payload) VALUES(0,'repo','y','{}')"); err != nil {
		t.Fatal(err)
	}
	var seq int
	if err := s.db.QueryRow("SELECT seq FROM feed").Scan(&seq); err != nil || seq != 2 {
		t.Fatalf("feed sequence: %d %v", seq, err)
	}
}

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
