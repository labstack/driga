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
	"github.com/labstack/driga/internal/signatures"
	"github.com/labstack/driga/internal/store"
)

// verifyOrphanOwnership runs after consent and the idle window. A stored
// explanation or Verified flag cannot authorize killing a live session's helper.
func verifyOrphanOwnership(ctx context.Context, g Environment, plat platform.Platform, o model.Orphan, sigs *signatures.Set) error {
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
	// Procs omits working directories on native platforms. Read both the target
	// and native agents afresh, including agents absent from stored sessions.
	pids := []int32{o.RootPID}
	requested := map[int32]bool{o.RootPID: true}
	for _, p := range idx.Descendants(o.RootPID) {
		pids = append(pids, p.PID)
		requested[p.PID] = true
	}
	sourceOS := platform.ProcessOS(plat)
	classifier := derive.NewClassifier(sigs, len(procs))
	for _, agent := range derive.NativeAgentProcesses(procs, root, sourceOS, classifier) {
		if !requested[agent.PID] {
			pids = append(pids, agent.PID)
			requested[agent.PID] = true
		}
	}
	cwds, err := plat.Cwds(ctx, pids)
	if err != nil {
		return fmt.Errorf("cannot verify orphan ownership directories: %w", err)
	}
	for _, pid := range pids {
		if !filepath.IsAbs(cwds[pid]) {
			return fmt.Errorf("cannot verify orphan ownership: current working directory unavailable for pid %d", pid)
		}
	}
	for i := range procs {
		if requested[procs[i].PID] {
			cwd := cwds[procs[i].PID]
			if filepath.IsAbs(cwd) {
				cwd = gitscan.NormalizePath(cwd)
			}
			procs[i].Cwd = cwd
		}
	}
	idx = derive.NewProcIndex(procs)
	if reason := derive.OrphanOwnershipReason(idx, sessions, o.RootPID); reason != "" {
		return fmt.Errorf("%s", reason)
	}
	root, _ = idx.Get(o.RootPID)
	tree := append([]model.Proc{root}, idx.Descendants(o.RootPID)...)
	agents := derive.NativeAgentProcesses(procs, root, sourceOS, classifier)
	if reason := derive.NativeAgentOwnershipReason(idx, tree, agents, root, sourceOS); reason != "" {
		return fmt.Errorf("%s", reason)
	}
	return nil
}
