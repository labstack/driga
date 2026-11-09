package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/gitscan"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/reclaim"
	"github.com/labstack/driga/internal/signatures"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

type reclaimInspection struct {
	Platform              platform.Platform
	Processes, Simulators bool
	PID                   int32
	MinAge                time.Duration
	Tool                  func(context.Context, string, ...string) ([]byte, error)
}

// Fast native review never falls back to transcript collection or Git scans.
func openReclaimReader(ctx context.Context, g *Globals, fast bool) (*store.Store, string, func(), error) {
	if !fast {
		return OpenReader(ctx, g)
	}
	if err := ctx.Err(); err != nil {
		return nil, "", nil, err
	}
	path := filepath.Join(config.DataDir(), "driga.db")
	mode, storage := "daemon", store.ReadOnly
	if _, err := os.Stat(path); os.IsNotExist(err) {
		mode, storage = "snapshot", store.Memory
	} else if err != nil {
		return nil, "", nil, err
	}
	st, err := store.Open(path, storage)
	if err != nil {
		return nil, "", nil, err
	}
	return st, mode, func() { _ = st.Close() }, nil
}

func inspectReclaimProcesses(ctx context.Context, tx *store.Tx, inspection reclaimInspection, rows []reclaim.Opportunity, stored []model.Proc, sessions []model.Session, orphans []view.OrphanRow) ([]reclaim.Opportunity, error) {
	inspectCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	procs, err := inspection.Platform.Procs(inspectCtx)
	if err != nil {
		return nil, fmt.Errorf("current process inventory unavailable: %w", err)
	}
	now := time.Now()
	var self model.Proc
	for _, p := range procs {
		if p.PID == int32(os.Getpid()) {
			self = p
			break
		}
	}
	if inspection.PID != 0 {
		rows = []reclaim.Opportunity{}
	}
	// Owner metadata is used only for this native read, never stored or shared.
	pids := make([]int32, 0, len(procs))
	for _, p := range procs {
		if !p.Zombie && (!p.OwnerKnown || !self.OwnerKnown || p.OwnerUID == self.OwnerUID) {
			pids = append(pids, p.PID)
		}
	}
	cwds, cwdErr := inspection.Platform.Cwds(inspectCtx, pids)
	canonical := map[string]string{}
	normalize := func(path string) string {
		if !filepath.IsAbs(path) {
			return path
		}
		if value, ok := canonical[path]; ok {
			return value
		}
		value := gitscan.NormalizePath(path)
		canonical[path] = value
		return value
	}
	for i := range procs {
		if cwd := cwds[procs[i].PID]; cwd != "" {
			procs[i].Cwd = normalize(cwd)
		} else {
			procs[i].Cwd = normalize(procs[i].Cwd)
		}
	}
	for i := range sessions {
		sessions[i].Cwd = normalize(sessions[i].Cwd)
	}
	sources, err := tx.Sources()
	if err != nil {
		return nil, err
	}
	var cpuAt time.Time
	sessionsSeen, sessionsStale := false, false
	for _, source := range sources {
		if source.Name == "procs" {
			cpuAt = source.LastOKAt
		}
		switch source.Name {
		case "claude", "codex", "copilot", "gemini", "agy":
			sessionsSeen = true
			if source.LastOKAt.IsZero() || source.LastOKAt.After(now) || now.Sub(source.LastOKAt) > 3*collector.Cadence(time.Hour).Procs || source.LastErrorAt.After(source.LastOKAt) {
				sessionsStale = true
			}
		}
	}
	previous := make(map[int32]model.Proc, len(stored))
	for _, p := range stored {
		previous[p.PID] = p
	}
	cpuKnown := map[int32]bool{}
	for i, p := range procs {
		old, ok := previous[p.PID]
		if ok && !p.StartTime.IsZero() && p.StartTime.Equal(old.StartTime) && !cpuAt.IsZero() && !cpuAt.After(now) && now.Sub(cpuAt) <= 3*collector.Cadence(time.Hour).Procs {
			procs[i].CPUPercent, cpuKnown[p.PID] = old.CPUPercent, true
		}
	}
	in := reclaim.ProcessReviewInput{Processes: procs, Sessions: sessions, Orphans: orphans, SourceOS: platform.ProcessOS(inspection.Platform), Now: now, MinAge: inspection.MinAge, TargetPID: inspection.PID, Self: self, CPUKnown: cpuKnown}
	preliminary := reclaim.ReviewProcesses(in)
	if prober, ok := inspection.Platform.(platform.SocketProber); ok {
		in.Peers = map[int32]model.PeerState{}
		classifier := derive.NewClassifier(signatures.Builtin(), len(procs))
		current := make(map[int32]model.Proc, len(procs))
		for _, p := range procs {
			current[p.PID] = p
		}
		for _, row := range preliminary {
			if row.Status == "protected" {
				continue
			}
			// Probe separate trees separately: connections between two candidates
			// must still count as external clients, not internal tree traffic.
			members := make([]model.Proc, 0, len(row.PIDs))
			for _, pid := range row.PIDs {
				members = append(members, current[pid])
			}
			resolve := derive.NeedsResolvedPeers(classifier, members)
			_, resolverAvailable := prober.(platform.PeerResolver)
			peers, err := platform.ProbePeers(inspectCtx, prober, row.PIDs, resolve)
			if err == nil {
				for pid, state := range peers {
					if resolve && !resolverAvailable && (state == model.PeerListening || state == model.PeerSystem) {
						continue
					}
					in.Peers[pid] = state
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reviews := reclaim.ReviewProcesses(in)
	rows = reclaim.MergeProcessReviews(rows, reviews, time.Now())
	if inspection.PID != 0 && len(reviews) == 0 {
		rows = append(rows, reclaim.Opportunity{ID: fmt.Sprintf("process:%d", inspection.PID), Kind: "process_review", RootPID: inspection.PID, Status: "not_found", ObservedAt: time.Now(), Reason: "PID was not present in the current native snapshot; exit or identity could not be established"})
	}
	if cwdErr != nil {
		rows = append(rows, reclaim.Opportunity{ID: "coverage:cwds", Kind: "coverage", Status: "unavailable", Reason: "Current working directory inspection was incomplete; session ownership may be unknown."})
		for i := range rows {
			if rows[i].Status == "ready" && rows[i].Kind == "process" {
				rows[i].Status, rows[i].Action = "review", ""
				rows[i].Reason += "; current directory ownership inspection was incomplete"
			}
		}
	}
	if !self.OwnerKnown {
		rows = append(rows, reclaim.Opportunity{ID: "coverage:owner", Kind: "coverage", Status: "unavailable", Reason: "Current user identity was unavailable; general process discovery is incomplete. Inspect an exact --pid for details."})
	}
	if !sessionsSeen || sessionsStale {
		rows = append(rows, reclaim.Opportunity{ID: "coverage:sessions", Kind: "coverage", Status: "unavailable", Reason: "Stored session observations are unavailable or stale; native agent protection still applies. General review never authorizes shutdown."})
		for i := range rows {
			if rows[i].Kind == "process" && rows[i].Status == "ready" {
				rows[i].Status = "review"
				rows[i].Reason += "; session observation coverage is incomplete"
			}
		}
	}
	return rows, nil
}

func appendReclaimSimulators(ctx context.Context, inspection reclaimInspection, rows []reclaim.Opportunity) []reclaim.Opportunity {
	run := inspection.Tool
	if run == nil {
		run = runReclaimInventory
	}
	inspectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	devices, err := reclaim.CollectSimulators(inspectCtx, run, time.Now())
	if err != nil {
		return append(rows, reclaim.Opportunity{ID: "coverage:simulators", Kind: "coverage", Status: "unavailable", Reason: "Booted simulator inventory could not be inspected; device activity remains unknown."})
	}
	for _, device := range devices {
		rows = append(rows, reclaim.Opportunity{ID: "simulator:" + device.ID, Kind: "simulator", Name: device.Name, Category: device.Runtime, Status: device.Status, ObservedAt: device.ObservedAt, Reason: device.Reason, Simulator: &device})
	}
	return rows
}

type reclaimInventoryOutput struct {
	buf      bytes.Buffer
	exceeded bool
}

func (b *reclaimInventoryOutput) Write(p []byte) (int, error) {
	if len(p) > reclaim.MaxSimulatorInventoryBytes-b.buf.Len() {
		b.exceeded = true
		return 0, errors.New("simulator inventory exceeds output limit")
	}
	return b.buf.Write(p)
}

func runReclaimInventory(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	// Also bound inherited pipes after a command exits or is cancelled.
	cmd.WaitDelay = 2 * time.Second
	var out reclaimInventoryOutput
	cmd.Stdout, cmd.Stderr = &out, io.Discard
	err := platform.RunChild(cmd)
	if out.exceeded {
		return nil, errors.New("simulator inventory exceeds output limit")
	}
	if err != nil {
		return nil, err
	}
	return out.buf.Bytes(), nil
}
