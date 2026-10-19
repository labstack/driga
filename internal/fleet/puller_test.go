package fleet

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/driga/internal/feed"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func pullFixture(t *testing.T, overrides ...http.HandlerFunc) (*Puller, <-chan Batch, *store.Store, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	s, st, id := serverFixture(t)
	var snapshots, presence atomic.Int32
	presence.Store(-1)
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	cfg := PinnedTLS(s.id, func([32]byte) bool { return true })
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/snapshot" {
			snapshots.Add(1)
		}
		if r.URL.Path == "/v1/events" {
			if r.URL.Query().Get("presence") == "1" {
				presence.Store(1)
			} else {
				presence.Store(0)
			}
		}
		if len(overrides) > 0 {
			overrides[0](w, r)
			return
		}
		s.ServeHTTP(w, r)
	})}
	go srv.Serve(tls.NewListener(ln, cfg))
	t.Cleanup(func() { srv.Close() })
	out := make(chan Batch, 32)
	p := NewPuller(Peer{Name: "vm", MachineID: st.MachineID(), DeviceID: s.id.DeviceID, Addr: ln.Addr().String()}, id, out)
	return p, out, st, &snapshots, &presence
}
func nextBatch(t *testing.T, ch <-chan Batch, pred func(Batch) bool) Batch {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case b := <-ch:
			b.Complete(nil)
			if pred(b) {
				return b
			}
		case <-timer.C:
			t.Fatal("no pull batch")
			return Batch{}
		}
	}
}
func runPull(t *testing.T, p *Puller) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); p.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	return cancel
}
func TestRestartResumesWithoutResnapshot(t *testing.T) {
	p, out, st, count, _ := pullFixture(t)
	cancel := runPull(t, p)
	snap := nextBatch(t, out, func(b Batch) bool { return b.Snapshot != nil })
	viewerStore := remoteStore(t)
	apply(t, viewerStore, snap, time.Now().UTC())
	cancel()
	// Reconstruct the client from the writer's saved cursor, as a collector
	// restart does, rather than retaining an in-memory puller position.
	machines, e := viewerStore.Machines()
	if e != nil || len(machines) != 1 {
		t.Fatalf("cursor not persisted: %v %v", machines, e)
	}
	p2 := NewPuller(p.peer, p.id, p.out)
	p2.Resume(Cursor{Epoch: machines[0].FeedEpoch, Seq: machines[0].FeedSeq})
	runPull(t, p2)
	if e := st.Tx(context.Background(), func(tx *store.Tx) error {
		k, v := feed.Encode(feed.Event, model.Event{Adapter: "claude", ExternalID: "one", Kind: "Stop", TS: time.Now()}, feed.Titles)
		_, e := tx.AppendFeed("event", k, v, time.Now())
		return e
	}); e != nil {
		t.Fatal(e)
	}
	b := nextBatch(t, out, func(b Batch) bool { return len(b.Rows) > 0 })
	if b.Cursor.Seq != snap.Cursor.Seq+1 || count.Load() != 1 {
		t.Fatalf("cursor=%+v snapshots=%d", b.Cursor, count.Load())
	}
}
func TestEpochChangeResnapshots(t *testing.T) {
	p, out, st, count, _ := pullFixture(t)
	p.Resume(Cursor{"old-epoch", 100})
	runPull(t, p)
	b := nextBatch(t, out, func(b Batch) bool { return b.Snapshot != nil })
	epoch, _, _, _ := st.FeedBounds()
	if b.Cursor.Epoch != epoch || count.Load() != 1 {
		t.Fatalf("%+v snapshots=%d", b.Cursor, count.Load())
	}
	// Change an epoch while already following: forcing a reconnect must also
	// take a fresh snapshot rather than silently following the new sequence.
	if e := st.SetMeta("feed_epoch", "replacement"); e != nil {
		t.Fatal(e)
	}
	p.SetPresence(true)
	b = nextBatch(t, out, func(b Batch) bool { return b.Snapshot != nil && b.Cursor.Epoch == "replacement" })
	if count.Load() != 2 {
		t.Fatal(fmt.Sprint(b.Cursor, count.Load()))
	}
}
func TestPresenceOnlyWhenWatched(t *testing.T) {
	p, out, _, count, presence := pullFixture(t)
	runPull(t, p)
	nextBatch(t, out, func(b Batch) bool { return b.Snapshot != nil })
	for _, watched := range []bool{false, true, false} {
		p.SetPresence(watched)
		want := int32(0)
		if watched {
			want = 1
		}
		until := time.Now().Add(5 * time.Second)
		for presence.Load() != want && time.Now().Before(until) {
			select {
			case b := <-out:
				b.Complete(nil)
			case <-time.After(time.Millisecond):
			}
		}
		if presence.Load() != want {
			t.Fatalf("presence=%d want=%d", presence.Load(), want)
		}
	}
	if count.Load() != 1 {
		t.Fatalf("presence resnapshotted %d", count.Load())
	}
}

func TestPullerIncompatibleSchema(t *testing.T) {
	var machine string
	p, out, st, count, _ := pullFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"schema":1,"machine_id":%q}`, machine)
	})
	machine = st.MachineID()
	runPull(t, p)
	b := nextBatch(t, out, func(b Batch) bool { return b.State == "incompatible" })
	if b.Snapshot != nil || len(b.Rows) != 0 || count.Load() != 0 {
		t.Fatal("incompatible peer applied rows")
	}
}
func TestPullerStopsOn204(t *testing.T) {
	var machine string
	p, out, st, count, _ := pullFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/hello":
			fmt.Fprintf(w, `{"schema":2,"machine_id":%q}`, machine)
		case "/v1/snapshot":
			fmt.Fprintf(w, `{"schema":2,"observed_at":%q,"data":{"machine":{"machine_id":%q},"cursor":{"epoch":"epoch","seq":0}}}`, time.Now().UTC().Format(time.RFC3339Nano), machine)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	})
	machine = st.MachineID()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()
	nextBatch(t, out, func(b Batch) bool { return b.Snapshot != nil })
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("204 did not stop puller")
	}
	if count.Load() != 1 {
		t.Fatal(count.Load())
	}
}

func TestViewerMismatchMessageNamesRekey(t *testing.T) {
	p, out, _, _, _ := pullFixture(t)
	p.peer.MachineID = "recreated-machine"
	runPull(t, p)
	b := nextBatch(t, out, func(b Batch) bool { return b.State == "untrusted" })
	if !strings.Contains(b.Error, "driga fleet rekey") || !strings.Contains(b.Error, "on the peer") {
		t.Fatalf("missing peer recovery instruction: %s", b.Error)
	}
}

func TestPullerBatchesBurst(t *testing.T) {
	var peerID, epoch string
	p, out, st, snapshots, _ := pullFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/hello":
			json.NewEncoder(w).Encode(hello{Schema: 2, MachineID: peerID, FeedEpoch: epoch, ObservedAt: time.Now()})
		case "/v1/events":
			w.Header().Set("Content-Type", "text/event-stream")
			for i := 1; i <= 1000; i++ {
				_, payload := feed.Encode(feed.Session, model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: fmt.Sprint(i)}}, feed.Titles)
				if err := writeFrame(w, epoch, FeedRow{Seq: int64(i), Kind: string(feed.Session), Payload: payload}); err != nil {
					return
				}
			}
		default:
			http.Error(w, "unexpected resnapshot", 500)
		}
	})
	peerID = p.peer.MachineID
	epoch, _, _, _ = st.FeedBounds()
	p.Resume(Cursor{Epoch: epoch})
	viewer := remoteStore(t)
	if err := viewer.Tx(context.Background(), func(tx *store.Tx) error { return tx.ForMachine(peerID).UpsertMachine(model.Machine{FeedEpoch: epoch}) }); err != nil {
		t.Fatal(err)
	}
	cancel := runPull(t, p)
	defer cancel()
	rows, transactions := 0, 0
	for rows < 1000 {
		select {
		case b := <-out:
			if len(b.Rows) == 0 {
				b.Complete(nil)
				continue
			}
			transactions++
			if transactions > 4 {
				b.Complete(context.Canceled)
				t.Fatalf("1000-row burst required more than 4 transactions (%d rows so far)", rows)
			}
			// A collector wake commits while the stream reader buffers the burst.
			time.Sleep(10 * time.Millisecond)
			err := viewer.Tx(context.Background(), func(tx *store.Tx) error { return ApplyBatch(tx, b, time.Now()) })
			b.Complete(err)
			if err != nil {
				t.Fatal(err)
			}
			rows += len(b.Rows)
		case <-time.After(5 * time.Second):
			t.Fatal("burst did not apply")
		}
	}
	sessions, err := viewer.SessionsScoped(store.SessionFilter{IncludeEnded: true}, store.Scope{Machines: []string{peerID}})
	if err != nil || len(sessions) != 1000 || snapshots.Load() != 0 {
		t.Fatalf("sessions=%d snapshots=%d err=%v", len(sessions), snapshots.Load(), err)
	}
	t.Logf("1000 rows committed in %d transactions without resnapshot", transactions)
}
