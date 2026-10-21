package fleet

import (
	"context"
	"fmt"
	"github.com/labstack/driga/internal/filelock"
	"os"
	"path/filepath"
	"time"

	"github.com/labstack/driga/internal/store"
)

// identityLock coordinates maintenance with the collector without signaling it.
func identityLock(dataDir string) (*os.File, error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dataDir, "driga.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return nil, err
	}
	if err := filelock.Lock(f, true); err != nil {
		f.Close()
		return nil, fmt.Errorf("stop the collector before identity maintenance or purging: %w", err)
	}
	return f, nil
}

// Rekey regenerates the certificate and clears all pairings. The collector must
// be stopped so no listener continues serving with the old identity.
func Rekey(ctx context.Context, dataDir string) error { return resetIdentity(ctx, dataDir, false) }

// ResetID regenerates the machine ID and certificate and re-stamps local rows.
func ResetID(ctx context.Context, dataDir string) error { return resetIdentity(ctx, dataDir, true) }
func resetIdentity(ctx context.Context, dataDir string, restamp bool) error {
	lock, err := identityLock(dataDir)
	if err != nil {
		return err
	}
	defer lock.Close()
	identityMu.Lock()
	defer identityMu.Unlock()
	dir := filepath.Join(dataDir, "fleet")
	if err := privateDir(dir); err != nil {
		return err
	}
	st, err := store.Open(filepath.Join(dataDir, "driga.db"), store.ReadWrite)
	if err != nil {
		return err
	}
	defer st.Close()
	// A crash or write failure across the DB/certificate boundary fails closed.
	// Retrying reset-id repairs it without ever accepting an old certificate.
	marker := filepath.Join(dir, "reset_pending")
	if err := atomicPrivateWrite(marker, []byte("identity maintenance pending\n")); err != nil {
		return err
	}
	if restamp {
		if err := st.ResetMachineID(ctx); err != nil {
			return err
		}
	}
	if _, err := createIdentity(dir, st.MachineID()); err != nil {
		return err
	}
	trustMu.Lock()
	defer trustMu.Unlock()
	if err := SavePeers(dir, nil); err != nil {
		return err
	}
	if err := SaveViewers(dir, nil); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dir, "enroll.json")); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Remove(marker)
}

// RemovePeer removes local trust. Remote rows remain unless purge is requested.
// Pullers observe the atomically replaced peers.json through their reload path.
func RemovePeer(ctx context.Context, dataDir, name string, purge bool) (Peer, error) {
	var zero Peer
	trustMu.Lock()
	defer trustMu.Unlock()
	dir := filepath.Join(dataDir, "fleet")
	ps, err := LoadPeers(dir)
	if err != nil {
		return zero, err
	}
	keep := make([]Peer, 0, len(ps))
	var removed *Peer
	for _, p := range ps {
		if p.Name == name || p.DeviceID == name {
			if removed != nil {
				return zero, fmt.Errorf("ambiguous peer %q", name)
			}
			v := p
			removed = &v
		} else {
			keep = append(keep, p)
		}
	}
	if removed == nil {
		return zero, fmt.Errorf("peer %q not found", name)
	}
	if err := SavePeers(dir, keep); err != nil {
		return zero, err
	}
	if purge {
		if _, controlErr := Control(ctx, dataDir, ControlRequest{Op: "purge", MachineID: removed.MachineID}); controlErr != nil {
			lock, err := identityLock(dataDir)
			if err != nil {
				return *removed, fmt.Errorf("peer removed, purge failed: %w", controlErr)
			}
			defer lock.Close()
			if err := purgePeer(ctx, dataDir, removed.MachineID); err != nil {
				return *removed, err
			}
		}
	}
	return *removed, nil
}

type purgeRequest struct {
	ctx     context.Context
	machine string
	done    chan error
}

func (s *Server) requestPurge(ctx context.Context, machine string) error {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	req := purgeRequest{ctx: ctx, machine: machine, done: make(chan error, 1)}
	select {
	case s.purges <- req:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-req.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ProcessControl performs queued maintenance between collector passes. It must
// be called only by the collector's writer loop, never by a network goroutine.
func (s *Server) ProcessControl(ctx context.Context, writers ...*store.Store) {
	for {
		select {
		case req := <-s.purges:
			if ctx.Err() != nil {
				req.done <- ctx.Err()
				continue
			}
			if req.ctx.Err() != nil {
				req.done <- req.ctx.Err()
				continue
			}
			if len(writers) > 0 {
				req.done <- writers[0].Tx(req.ctx, func(tx *store.Tx) error { return tx.PurgeRemote(req.machine) })
			} else {
				req.done <- purgePeer(req.ctx, s.dataDir, req.machine)
			}
		default:
			return
		}
	}
}
func purgePeer(ctx context.Context, dataDir, machine string) error {
	st, err := store.Open(filepath.Join(dataDir, "driga.db"), store.ReadWrite)
	if err != nil {
		return err
	}
	defer st.Close()
	return st.Tx(ctx, func(tx *store.Tx) error { return tx.PurgeRemote(machine) })
}

// RevokeViewer resolves a viewer and revokes through the running collector, or
// updates its trust offline while holding the collector lock.
func RevokeViewer(ctx context.Context, dataDir, name string) (Viewer, error) {
	var zero Viewer
	dir := filepath.Join(dataDir, "fleet")
	vs, err := LoadViewers(dir)
	if err != nil {
		return zero, err
	}
	var target *Viewer
	for _, v := range vs {
		if v.Name == name || v.DeviceID == name {
			if target != nil {
				return zero, fmt.Errorf("ambiguous viewer %q", name)
			}
			copy := v
			target = &copy
		}
	}
	if target == nil {
		return zero, fmt.Errorf("viewer %q not found", name)
	}
	if _, err := Control(ctx, dataDir, ControlRequest{Op: "revoke", DeviceID: target.DeviceID}); err == nil {
		return *target, nil
	}
	lock, err := identityLock(dataDir)
	if err != nil {
		return zero, err
	}
	defer lock.Close()
	trustMu.Lock()
	defer trustMu.Unlock()
	vs, err = LoadViewers(dir)
	if err != nil {
		return zero, err
	}
	keep := make([]Viewer, 0, len(vs))
	for _, v := range vs {
		if v.DeviceID != target.DeviceID {
			keep = append(keep, v)
		}
	}
	if err := SaveViewers(dir, keep); err != nil {
		return zero, err
	}
	return *target, nil
}
