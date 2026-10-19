package collector

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/labstack/driga/internal/fleet"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/spool"
	"github.com/labstack/driga/internal/store"
)

type ackFixture struct {
	a, b       Deps
	sa, sb     *store.Store
	dirA, dirB string
	idA, idB   fleet.Identity
	pullers    *fleetPullers
	server     *fleet.Server
	stop       func()
	ctx        context.Context
}

func newAckFixture(t *testing.T) *ackFixture {
	t.Helper()
	a, sa, _, _ := setup(t)
	b, sb, _, _ := setup(t)
	f := &ackFixture{a: a, b: b, sa: sa, sb: sb, dirA: t.TempDir(), dirB: t.TempDir(), ctx: context.Background()}
	var err error
	// Identity and store machine IDs must match; use persistent test stores.
	f.idA, err = fleet.LoadOrCreateIdentity(filepath.Join(f.dirA, "fleet"))
	if err != nil {
		t.Fatal(err)
	}
	f.idB, err = fleet.LoadOrCreateIdentity(filepath.Join(f.dirB, "fleet"))
	if err != nil {
		t.Fatal(err)
	}
	f.sa, err = store.Open(filepath.Join(f.dirA, "driga.db"), store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	f.sb, err = store.Open(filepath.Join(f.dirB, "driga.db"), store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	// The fixture starts a fleet server, so its producer must publish feed rows.
	f.a.Cfg.Fleet.Serve = true
	f.a.SpoolDir = filepath.Join(f.dirA, "spool")
	f.b.SpoolDir = filepath.Join(f.dirB, "spool")
	if _, err = Pass(f.ctx, f.a, f.sa, Plan{Machine: true}); err != nil {
		t.Fatal(err)
	}
	if err = f.sa.Tx(f.ctx, func(tx *store.Tx) error {
		ss, err := tx.Sessions(store.SessionFilter{IncludeEnded: true})
		if err != nil {
			return err
		}
		return tx.UpsertInboxItem(model.InboxItem{ID: "item", SessionID: ss[0].ID, TS: a.now()})
	}); err != nil {
		t.Fatal(err)
	}
	// An identical local item must never be acknowledged by the remote request.
	if err = f.sb.Tx(f.ctx, func(tx *store.Tx) error { return tx.UpsertInboxItem(model.InboxItem{ID: "item", TS: b.now()}) }); err != nil {
		t.Fatal(err)
	}
	if err = fleet.SaveViewers(filepath.Join(f.dirA, "fleet"), []fleet.Viewer{{DeviceID: f.idB.DeviceID, Permissions: []string{"read", "ack"}}}); err != nil {
		t.Fatal(err)
	}
	f.pullers = newFleetPullers(f.dirB)
	t.Cleanup(func() {
		f.pullers.close()
		if f.stop != nil {
			f.stop()
		}
		f.sb.Close()
		f.sa.Close()
	})
	f.start(t)
	f.wait(t, func() bool {
		items, _ := f.sb.InboxScoped(time.Time{}, true, store.Scope{Machines: []string{f.sa.MachineID()}})
		return len(items) == 1
	})
	return f
}

func (f *ackFixture) start(t *testing.T) {
	t.Helper()
	ro, err := store.Open(filepath.Join(f.dirA, "driga.db"), store.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := fleet.NewServer(ro, f.idA, f.dirA, f.a.Cfg)
	s.SpoolDir = f.a.SpoolDir
	f.server = s
	ctx, cancel := context.WithCancel(f.ctx)
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln) }()
	f.stop = func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
		ro.Close()
	}
	// Persist the enrollment grant used by the viewer's local preflight.
	data, _ := json.Marshal(map[string]any{"name": "vm-1", "addr": ln.Addr().String(), "device_id": f.idA.DeviceID, "machine_id": f.sa.MachineID(), "ack": true})
	var peer fleet.Peer
	json.Unmarshal(data, &peer)
	if err := fleet.SavePeers(filepath.Join(f.dirB, "fleet"), []fleet.Peer{peer}); err != nil {
		t.Fatal(err)
	}
}

func (f *ackFixture) wait(t *testing.T, ready func() bool) {
	t.Helper()
	until := time.Now().Add(8 * time.Second)
	for time.Now().Before(until) {
		if err := f.pullers.wake(f.ctx, f.b, f.sb, false, false); err != nil {
			t.Fatal(err)
		}
		if ready() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	machines, _ := f.sb.Machines()
	t.Fatalf("remote ack did not converge: %+v", machines)
}

func (f *ackFixture) queue(t *testing.T) {
	t.Helper()
	data, _ := json.Marshal(map[string]any{"event": "ack", "message": "item", "machine_id": f.sa.MachineID(), "ts": f.b.now()})
	var ev model.HookEvent
	json.Unmarshal(data, &ev)
	if err := spool.Append(f.b.SpoolDir, ev); err != nil {
		t.Fatal(err)
	}
	if err := drainOnly(f.ctx, f.b, f.sb); err != nil {
		t.Fatal(err)
	}
}

func (f *ackFixture) pending(t *testing.T) bool {
	t.Helper()
	items, err := f.sb.InboxScoped(time.Time{}, true, store.Scope{Machines: []string{f.sa.MachineID()}})
	if err != nil || len(items) != 1 {
		t.Fatalf("items %+v: %v", items, err)
	}
	return items[0].AckPendingAt != nil && items[0].AckedAt == nil
}

func (f *ackFixture) finish(t *testing.T) {
	t.Helper()
	f.wait(t, func() bool { events, _, _ := spool.Drain(f.a.SpoolDir, nil); return len(events) > 0 })
	events, _, err := spool.Drain(f.a.SpoolDir, nil)
	if err != nil || events[0].Adapter != "fleet" || events[0].Message != "item" {
		t.Fatalf("peer spool: %+v %v", events, err)
	}
	if err := drainOnly(f.ctx, f.a, f.sa); err != nil {
		t.Fatal(err)
	}
	f.server.Notify()
	f.wait(t, func() bool {
		items, _ := f.sb.InboxScoped(time.Time{}, true, store.Scope{Machines: []string{f.sa.MachineID()}})
		return len(items) == 1 && items[0].AckedAt != nil && items[0].AckPendingAt == nil
	})
	items, _ := f.sb.Inbox(time.Time{}, true)
	if len(items) != 1 || items[0].AckedAt != nil {
		t.Fatalf("remote ack changed local inbox: %+v", items)
	}
}

func TestRemoteAckRoundTrip(t *testing.T) {
	f := newAckFixture(t)
	f.queue(t)
	if !f.pending(t) {
		t.Fatal("remote ack was not marked pending")
	}
	f.finish(t)
}

func TestRemoteAckPendingWhileUnreachable(t *testing.T) {
	f := newAckFixture(t)
	f.stop()
	f.stop = nil
	f.queue(t)
	f.queue(t)
	f.wait(t, func() bool {
		ms, _ := f.sb.Machines()
		for _, m := range ms {
			if m.MachineID == f.sa.MachineID() {
				return m.LastError != ""
			}
		}
		return false
	})
	if !f.pending(t) {
		t.Fatal("unreachable ack was lost")
	}
	// Restart the viewer, then change the peer address to force reconnect and
	// a fresh snapshot. Both must preserve the durable pending request.
	f.pullers.close()
	f.pullers = newFleetPullers(f.dirB)
	if err := f.sb.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	f.sb, err = store.Open(filepath.Join(f.dirB, "driga.db"), store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.sa.Tx(f.ctx, func(tx *store.Tx) error { return tx.SetMeta("feed_epoch", "replacement-epoch") }); err != nil {
		t.Fatal(err)
	}
	f.start(t)
	f.wait(t, func() bool {
		ms, _ := f.sb.Machines()
		for _, m := range ms {
			if m.MachineID == f.sa.MachineID() {
				return m.FeedEpoch == "replacement-epoch"
			}
		}
		return false
	})
	if !f.pending(t) {
		t.Fatal("resnapshot lost pending ack")
	}
	f.finish(t)
	items, _ := f.sa.Inbox(time.Time{}, true)
	at := *items[0].AckedAt
	if err := spool.Append(f.a.SpoolDir, model.HookEvent{Event: "ack", Adapter: "fleet", Message: "item", TS: at.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if err := drainOnly(f.ctx, f.a, f.sa); err != nil {
		t.Fatal(err)
	}
	items, _ = f.sa.Inbox(time.Time{}, true)
	if !items[0].AckedAt.Equal(at) {
		t.Fatal("duplicate ack changed receipt")
	}
}
