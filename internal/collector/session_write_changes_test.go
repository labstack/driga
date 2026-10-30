package collector

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestPassUpsertsOnlyChangedSessions(t *testing.T) {
	d, _, _, a := setup(t)
	d.Notifier = &fakeNotifier{}
	path := filepath.Join(t.TempDir(), "db")
	st, err := store.Open(path, store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE writes(n INTEGER); INSERT INTO writes VALUES(0); CREATE TRIGGER count_updates AFTER UPDATE ON sessions BEGIN UPDATE writes SET n=n+1; END;`)
	if err != nil {
		t.Fatal(err)
	}
	pass(t, d, st, Plan{Procs: true, Sockets: true})
	if _, err = db.Exec("UPDATE writes SET n=0"); err != nil {
		t.Fatal(err)
	}
	d.Now = func() time.Time { return time.Date(2026, 10, 18, 12, 0, 1, 0, time.UTC) }
	pass(t, d, st, Plan{})
	var n int
	if err := db.QueryRow("SELECT n FROM writes").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("unchanged session updated %d times", n)
	}
	if d.State.sessionRevision != st.Revision() {
		t.Fatal("session-neutral alert transaction invalidated session cache")
	}
	a.obs[0].Title = "changed"
	pass(t, d, st, Plan{})
	if err := db.QueryRow("SELECT n FROM writes").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("changed session writes=%d", n)
	}
}

func TestPassSessionCacheSeesLocalAndExternalWrites(t *testing.T) {
	d, _, _, a := setup(t)
	path := filepath.Join(t.TempDir(), "db")
	st, err := store.Open(path, store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	pass(t, d, st, Plan{Procs: true})
	a.obs = nil
	for _, external := range []bool{false, true} {
		writer := st
		if external {
			writer, err = store.Open(path, store.ReadWrite)
			if err != nil {
				t.Fatal(err)
			}
		}
		err = writer.Tx(context.Background(), func(tx *store.Tx) error {
			ss, err := tx.Sessions(store.SessionFilter{IncludeEnded: true})
			if err != nil {
				return err
			}
			ss[0].Title = "written outside pass"
			if external {
				ss[0].Title = "external connection"
			}
			_, err = tx.UpsertSession(ss[0])
			return err
		})
		if external {
			writer.Close()
		}
		if err != nil {
			t.Fatal(err)
		}
		pass(t, d, st, Plan{})
		want := "written outside pass"
		if external {
			want = "external connection"
		}
		if len(d.State.sessions) != 1 || d.State.sessions[0].Title != want {
			t.Fatalf("cache failed to invalidate: %+v", d.State.sessions)
		}
	}
}

func TestSessionRowComparisonKeepsFacts(t *testing.T) {
	at := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	base := model.Session{ID: 1, SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "one"}, Mixer: &model.StateMixer{ObservedAt: at, Input: &model.StateInput{}, Verdict: "idle"}}
	later := base
	m := *base.Mixer
	m.ObservedAt = at.Add(time.Second)
	later.Mixer = &m
	if !sameSessionRow(base, later) {
		t.Fatal("clock alone changed row")
	}
	for _, mutate := range []func(*model.Session){
		func(s *model.Session) { s.Title = "changed" },
		func(s *model.Session) { s.RegistryUpdatedAt = at },
		func(s *model.Session) { s.LastPrompt = "changed" },
		func(s *model.Session) { s.Label = "stuck" },
		func(s *model.Session) { id := int64(2); s.ParentID = &id },
		func(s *model.Session) {
			m := *s.Mixer
			in := *m.Input
			in.Proc = &model.Proc{PID: 42, CPUPercent: 7}
			m.Input = &in
			s.Mixer = &m
		},
		func(s *model.Session) { m := *s.Mixer; m.Agreement++; s.Mixer = &m },
	} {
		changed := later
		mutate(&changed)
		if sameSessionRow(base, changed) {
			t.Fatalf("changed fact suppressed: %+v", changed)
		}
	}
}
