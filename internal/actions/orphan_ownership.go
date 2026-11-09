package actions

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/gitscan"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/store"
)

// verifyOrphanOwnership runs after consent and the idle window. A stored
// explanation or Verified flag cannot authorize killing a live session's helper.
func verifyOrphanOwnership(ctx context.Context, g Environment, plat platform.Platform, o model.Orphan) error {
	if strings.HasPrefix(o.ID, "session:") {
		return nil // Explicit session kills have a separate identity-checked selection.
	}
	var st *store.Store
	var closeReader func()
	var err error
	if g.OpenReader != nil {
		st, _, closeReader, err = g.OpenReader(ctx)
	} else {
		st, err = store.Open(filepath.Join(config.DataDir(), "driga.db"), store.ReadOnly)
		closeReader = func() { _ = st.Close() }
	}
	if err != nil {
		return fmt.Errorf("cannot verify orphan ownership: %w", err)
	}
	defer closeReader()
	sessions, err := st.Sessions(store.SessionFilter{IncludeEnded: true})
	if err != nil {
		return fmt.Errorf("cannot verify orphan ownership: %w", err)
	}
	// Hook-only rows can retain a logical directory spelling until an adapter
	// observation arrives. Resolve local aliases at the native action seam.
	for i := range sessions {
		if filepath.IsAbs(sessions[i].Cwd) {
			sessions[i].Cwd = gitscan.NormalizePath(sessions[i].Cwd)
		}
	}
	procs, err := plat.Procs(ctx)
	if err != nil {
		return fmt.Errorf("cannot verify orphan ownership: %w", err)
	}
	idx := derive.NewProcIndex(procs)
	root, present := idx.Get(o.RootPID)
	if !present || !root.StartTime.Equal(o.StartTime) {
		return fmt.Errorf("orphan ownership changed: root exited or identity changed")
	}
	// Procs omits working directories on native platforms. Read them afresh for
	// the target tree rather than borrowing the displayed snapshot's directory.
	pids := []int32{o.RootPID}
	for _, p := range idx.Descendants(o.RootPID) {
		pids = append(pids, p.PID)
	}
	cwds, err := plat.Cwds(ctx, pids)
	if err != nil {
		return fmt.Errorf("cannot verify orphan ownership directories: %w", err)
	}
	for i := range procs {
		if cwd := cwds[procs[i].PID]; cwd != "" {
			if filepath.IsAbs(cwd) {
				cwd = gitscan.NormalizePath(cwd)
			}
			procs[i].Cwd = cwd
		}
	}
	if reason := derive.OrphanOwnershipReason(derive.NewProcIndex(procs), sessions, o.RootPID); reason != "" {
		return fmt.Errorf("%s", reason)
	}
	return nil
}
