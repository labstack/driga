package collector

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/labstack/driga/internal/fleet"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/scrub"
	"github.com/labstack/driga/internal/store"
)

type peerPull struct {
	peer   fleet.Peer
	puller *fleet.Puller
	cancel context.CancelFunc
	done   chan struct{}
}
type fleetPullers struct {
	dir         string
	stamp       os.FileInfo
	peers       map[string]*peerPull
	batches     chan fleet.Batch
	ready       []fleet.Batch
	identity    *fleet.Identity
	control     *fleet.Server
	server      *fleet.Server
	stopControl func()
}

func newFleetPullers(dataDir string) *fleetPullers {
	return &fleetPullers{dir: dataDir, peers: make(map[string]*peerPull), batches: make(chan fleet.Batch, 256)}
}
func (f *fleetPullers) close() {
	for _, p := range f.peers {
		p.cancel()
	}
	for _, p := range f.peers {
		<-p.done
	}
	if f.stopControl != nil {
		f.stopControl()
	}
}
func (f *fleetPullers) reload(ctx context.Context, d Deps, st *store.Store, hasServer bool) error {
	path := filepath.Join(f.dir, "fleet", "peers.json")
	stamp, err := os.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if f.stamp != nil && stamp != nil && os.SameFile(f.stamp, stamp) && f.stamp.ModTime() == stamp.ModTime() && f.stamp.Size() == stamp.Size() {
		return nil
	}
	peers, err := fleet.LoadPeers(filepath.Dir(path))
	if err != nil {
		return err
	}
	if len(peers) > 0 && f.identity == nil {
		id, err := fleet.LoadOrCreateIdentity(filepath.Dir(path))
		if err != nil {
			return err
		}
		f.identity = &id
	}
	wanted := make(map[string]fleet.Peer)
	for _, p := range peers {
		if p.MachineID == "" || p.MachineID == st.MachineID() {
			continue
		}
		if previous, ok := wanted[p.MachineID]; ok && previous.DeviceID != p.DeviceID {
			p.State = "untrusted"
		}
		wanted[p.MachineID] = p
	}
	for id, running := range f.peers {
		if p, ok := wanted[id]; !ok || p != running.peer {
			running.cancel()
			<-running.done
			delete(f.peers, id)
			if !ok {
				if err := st.Tx(ctx, func(tx *store.Tx) error {
					if _, err := tx.ForMachine(id).Machine(); errors.Is(err, store.ErrNotFound) {
						return nil
					} else if err != nil {
						return err
					}
					return fleet.ApplyBatch(tx, fleet.Batch{MachineID: id, State: "stale", Error: "peer removed"}, d.now())
				}); err != nil {
					return err
				}
			}
		}
	}
	var machines []model.Machine
	if len(wanted) > 0 {
		machines, err = st.Machines()
		if err != nil {
			return err
		}
	}
	for id, p := range wanted {
		if _, ok := f.peers[id]; ok {
			continue
		}
		puller := fleet.NewPuller(p, *f.identity, f.batches)
		for _, m := range machines {
			if m.MachineID == id {
				puller.Resume(fleet.Cursor{Epoch: m.FeedEpoch, Seq: m.FeedSeq})
			}
		}
		runCtx, cancel := context.WithCancel(ctx)
		running := &peerPull{peer: p, puller: puller, cancel: cancel, done: make(chan struct{})}
		f.peers[id] = running
		go func() { defer close(running.done); puller.Run(runCtx) }()
	}
	// View-only collectors still expose local maintenance, including remove
	// --purge; this creates no TCP listener and no enrollment window.
	if len(peers) > 0 && !hasServer && f.control == nil {
		ro, err := store.Open(filepath.Join(f.dir, "driga.db"), store.ReadOnly)
		if err != nil {
			return err
		}
		control := fleet.NewServer(ro, *f.identity, f.dir, d.Cfg)
		stop, err := control.StartControl(ctx)
		if err != nil {
			ro.Close()
			return err
		}
		f.control = control
		f.stopControl = func() { stop(); ro.Close() }
	}
	f.stamp = stamp
	return nil
}
func (f *fleetPullers) wake(ctx context.Context, d Deps, st *store.Store, hasServer bool, watched bool) error {
	if err := f.reload(ctx, d, st, hasServer); err != nil {
		slog.Warn("fleet peers reload failed", "event", "fleet_peers_reload_failed", "err", scrub.Scrub(err.Error()))
	}
	for _, p := range f.peers {
		p.puller.SetPresence(watched)
	}
	if err := f.applyReady(ctx, d, st); err != nil {
		return err
	}
	if f.control != nil {
		f.control.ProcessControl(ctx, st)
	}
	if len(f.peers) == 0 {
		return nil
	}
	now := d.now()
	if err := st.Tx(ctx, func(tx *store.Tx) error { return fleet.RefreshStates(tx, now) }); err != nil {
		return err
	}
	acks, err := st.PendingAcks(store.Scope{})
	if err != nil {
		return err
	}
	byMachine := make(map[string][]string)
	for _, item := range acks {
		byMachine[item.MachineID] = append(byMachine[item.MachineID], item.ID)
	}
	for id, p := range f.peers {
		p.puller.SetPendingAcks(byMachine[id])
	}
	return nil
}

// applyReady handles channel-driven work without repeating periodic filesystem
// checks, stale-peer scans or pending-ack queries for every incoming batch.
func (f *fleetPullers) applyReady(ctx context.Context, d Deps, st *store.Store) error {
	pending := make([]fleet.Batch, 0, len(f.batches)+len(f.ready))
	incoming := f.ready
	f.ready = nil
	for _, b := range incoming {
		p, ok := f.peers[b.MachineID]
		if !ok || b.Peer == nil || *b.Peer != p.peer {
			b.Complete(context.Canceled)
			continue
		}
		pending = append(pending, b)
	}
	for range cap(f.batches) {
		select {
		case b := <-f.batches:
			p, ok := f.peers[b.MachineID]
			if !ok || b.Peer == nil || *b.Peer != p.peer {
				b.Complete(context.Canceled)
				continue
			}
			pending = append(pending, b)
		default:
			goto apply
		}
	}
apply:
	now := d.now()
	for _, b := range pending {
		err := st.Tx(ctx, func(tx *store.Tx) error { return fleet.ApplyBatch(tx, b, now) })
		if err == nil && b.Resnapshot && b.Snapshot != nil {
			if f.server != nil {
				f.server.NoteResnapshot()
			} else if f.control != nil {
				f.control.NoteResnapshot()
			}
		}
		b.Complete(err)
		if err != nil && ctx.Err() == nil {
			// Each peer owns its transaction and completion result. A malformed
			// batch leaves its cursor unchanged without rolling back other peers.
			slog.Warn("fleet apply failed", "event", "fleet_apply_failed", "machine_id", b.MachineID, "err", scrub.Scrub(err.Error()))
		}
	}
	return nil
}
