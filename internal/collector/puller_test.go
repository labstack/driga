package collector

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/fleet"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/spool"
	"github.com/labstack/driga/internal/store"
)

func twoCollectors(t *testing.T) (*store.Store, string, string, Deps, context.CancelFunc) {
	t.Helper()
	a, _, _, _ := setup(t)
	b, _, _, _ := setup(t)
	a.Now = nil
	b.Now = nil
	dirA, e := os.MkdirTemp("", "driga-a-")
	if e != nil {
		t.Fatal(e)
	}
	dirB, e := os.MkdirTemp("", "driga-b-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.RemoveAll(dirA); os.RemoveAll(dirB) })
	a.SpoolDir = filepath.Join(dirA, "spool")
	b.SpoolDir = filepath.Join(dirB, "spool")
	a.Cfg.Fleet.Serve = true
	a.Cfg.Fleet.Listen = "127.0.0.1:0"
	idA, e := fleet.LoadOrCreateIdentity(filepath.Join(dirA, "fleet"))
	if e != nil {
		t.Fatal(e)
	}
	idB, e := fleet.LoadOrCreateIdentity(filepath.Join(dirB, "fleet"))
	if e != nil {
		t.Fatal(e)
	}
	if e := fleet.SaveViewers(filepath.Join(dirA, "fleet"), []fleet.Viewer{{DeviceID: idB.DeviceID, Permissions: []string{"read"}}}); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	doneA := make(chan error, 1)
	doneB := make(chan error, 1)
	go func() { doneA <- Run(ctx, a, dirA) }()
	until := time.Now().Add(5 * time.Second)
	addr := ""
	for time.Now().Before(until) {
		v, e := os.ReadFile(filepath.Join(dirA, "fleet", "listen"))
		if e == nil {
			addr = strings.TrimSpace(string(v))
			break
		}
		time.Sleep(time.Millisecond)
	}
	if addr == "" {
		cancel()
		<-doneA
		t.Fatal("no listener")
	}
	if e := fleet.SavePeers(filepath.Join(dirB, "fleet"), []fleet.Peer{{Name: "vm", Addr: addr, DeviceID: idA.DeviceID, MachineID: idA.Cert.Leaf.Subject.CommonName}}); e != nil {
		cancel()
		<-doneA
		t.Fatal(e)
	}
	go func() { doneB <- Run(ctx, b, dirB) }()
	t.Cleanup(func() {
		cancel()
		if e := <-doneB; e != nil {
			t.Error(e)
		}
		if e := <-doneA; e != nil {
			t.Error(e)
		}
	})
	ro, e := store.Open(filepath.Join(dirB, "driga.db"), store.ReadOnly)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { ro.Close() })
	return ro, idA.Cert.Leaf.Subject.CommonName, dirA, a, cancel
}
func waitRemote(t *testing.T, st *store.Store, machine string) {
	t.Helper()
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		ss, e := st.SessionsScoped(store.SessionFilter{IncludeEnded: true}, store.Scope{Machines: []string{machine}})
		if e == nil && len(ss) > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("remote session never applied")
}
func TestTwoCollectorsFleet(t *testing.T) {
	ro, machine, _, _, _ := twoCollectors(t)
	waitRemote(t, ro, machine)
	// Remote apply deliberately precedes the first local collection. Wait
	// for both commits rather than assuming their completion order.
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		ss, e := ro.SessionsScoped(store.SessionFilter{IncludeEnded: true}, store.Scope{})
		if e == nil && len(ss) == 2 && ss[0].MachineID != ss[1].MachineID {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("local and remote sessions did not converge")
}
func TestHookToViewerUnder5s(t *testing.T) {
	ro, machine, _, a, _ := twoCollectors(t)
	waitRemote(t, ro, machine)
	start := time.Now()
	if e := spool.Append(a.SpoolDir, model.HookEvent{TS: start, Adapter: "claude", SessionID: "one", Event: "Notification"}); e != nil {
		t.Fatal(e)
	}
	until := start.Add(5 * time.Second)
	for time.Now().Before(until) {
		items, e := ro.InboxScoped(time.Time{}, false, store.Scope{Machines: []string{machine}})
		if e == nil && len(items) > 0 {
			t.Logf("hook to viewer: %s", time.Since(start))
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("hook not visible within 5s")
}

func TestViewerOnlyRemovePurgesAllRows(t *testing.T) {
	d, _, _, _ := setup(t)
	dir, e := os.MkdirTemp("", "driga-view-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	id, e := fleet.LoadOrCreateIdentity(filepath.Join(dir, "fleet"))
	if e != nil {
		t.Fatal(e)
	}
	// No network is needed: an explicitly untrusted peer remains visible until
	// removed, and still enables local purge maintenance for the viewer.
	peer := fleet.Peer{Name: "vm", MachineID: "remote", DeviceID: id.DeviceID, State: "untrusted"}
	if e := fleet.SavePeers(filepath.Join(dir, "fleet"), []fleet.Peer{peer}); e != nil {
		t.Fatal(e)
	}
	st, e := store.Open(filepath.Join(dir, "driga.db"), store.ReadWrite)
	if e != nil {
		t.Fatal(e)
	}
	if e := st.Tx(context.Background(), func(tx *store.Tx) error {
		r := tx.ForMachine("remote")
		if e := r.UpsertMachine(model.Machine{}); e != nil {
			return e
		}
		_, e := r.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "one"}})
		return e
	}); e != nil {
		t.Fatal(e)
	}
	st.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, d, dir) }()
	defer func() {
		cancel()
		if e := <-done; e != nil {
			t.Error(e)
		}
	}()
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		if _, e := fleet.Control(ctx, dir, fleet.ControlRequest{Op: "status"}); e == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if _, e := fleet.RemovePeer(ctx, dir, "vm", true); e != nil {
		t.Fatal(e)
	}
	// Let reload run after the purge, to catch recreation from queued batches.
	time.Sleep(1100 * time.Millisecond)
	ro, e := store.Open(filepath.Join(dir, "driga.db"), store.ReadOnly)
	if e != nil {
		t.Fatal(e)
	}
	defer ro.Close()
	ms, e := ro.Machines()
	if e != nil {
		t.Fatal(e)
	}
	for _, m := range ms {
		if m.MachineID == "remote" {
			t.Fatalf("purged machine reappeared: %+v", m)
		}
	}
	ss, e := ro.SessionsScoped(store.SessionFilter{IncludeEnded: true}, store.Scope{Machines: []string{"remote"}})
	if e != nil || len(ss) != 0 {
		t.Fatalf("purged sessions: %+v %v", ss, e)
	}
}
