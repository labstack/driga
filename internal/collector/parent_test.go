package collector

import (
	"context"
	"testing"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestParentExternalIDLinks(t *testing.T) {
	d, st, p, a := setup(t)
	// Deliberately allocate the child before its parent. Process ancestry
	// points to a different session; explicit adapter evidence must win.
	a.obs = []model.SessionObs{
		{Adapter: "claude", ExternalID: "child", ParentExternalID: "parent", PID: 102},
		{Adapter: "claude", ExternalID: "inferred", PID: 101},
		{Adapter: "claude", ExternalID: "parent"},
	}
	p.procs = append(p.procs, model.Proc{PID: 102, PPID: 101, Cmdline: "claude", Argv: []string{"claude"}})
	pass(t, d, st, Plan{Procs: true})
	assertParentExternalID(t, sessions(t, st))
}

func TestParentExternalIDLateParent(t *testing.T) {
	d, st, _, a := setup(t)
	a.obs = []model.SessionObs{{Adapter: "claude", ExternalID: "child", ParentExternalID: "parent"}}
	pass(t, d, st, Plan{})
	if s := sessions(t, st)[0]; s.ParentID != nil {
		t.Fatalf("missing parent linked: %+v", s)
	}
	// Only the parent is observed on the next pass. Pending evidence must
	// survive a fresh collector State, not just an adapter's memory.
	d.State = NewState()
	a.obs = []model.SessionObs{{Adapter: "claude", ExternalID: "parent"}}
	pass(t, d, st, Plan{})
	assertParentExternalID(t, sessions(t, st))
	pass(t, d, st, Plan{})
	assertParentExternalID(t, sessions(t, st))
}

func TestParentExternalIDScope(t *testing.T) {
	d, st, _, a := setup(t)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		_, err := tx.ForMachine("other-machine").UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "parent"}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	a.obs = []model.SessionObs{{Adapter: "claude", ExternalID: "child", ParentExternalID: "parent"}, {Adapter: "codex", ExternalID: "parent"}}
	pass(t, d, st, Plan{})
	for _, s := range sessions(t, st) {
		if s.ExternalID == "child" && s.ParentID != nil {
			t.Fatal("cross-adapter or cross-machine parent", s)
		}
	}
}

func assertParentExternalID(t *testing.T, ss []model.Session) {
	t.Helper()
	byID := map[string]model.Session{}
	for _, s := range ss {
		byID[s.ExternalID] = s
	}
	child, parent := byID["child"], byID["parent"]
	if child.ParentID == nil || *child.ParentID != parent.ID || parent.ID == 0 {
		t.Fatalf("child=%+v parent=%+v", child, parent)
	}
}
